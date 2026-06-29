package arch

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// MarshalOracleIntent Borsh-encodes an OracleIntent. Field order is wire-binding
// and matches dia_arch_shared::intent::OracleIntent declaration order.
func MarshalOracleIntent(intent OracleIntent) ([]byte, error) {
	w := &borshWriter{}
	w.writeString(intent.IntentType)
	w.writeString(intent.Version)
	w.writeBytes(intent.ChainID[:])
	w.writeBytes(intent.Nonce[:])
	w.writeBytes(intent.Expiry[:])
	w.writeString(intent.Symbol)
	w.writeBytes(intent.Price[:])
	w.writeBytes(intent.Timestamp[:])
	w.writeString(intent.Source)
	if err := w.writeByteVec(intent.Signature); err != nil {
		return nil, err
	}
	w.writeBytes(intent.Signer[:])
	return w.buf, nil
}

// UnmarshalOracleIntent decodes a Borsh-encoded OracleIntent.
func UnmarshalOracleIntent(data []byte) (OracleIntent, error) {
	r := &borshReader{buf: data}
	var out OracleIntent
	var err error
	if out.IntentType, err = r.readString(); err != nil {
		return out, fmt.Errorf("intentType: %w", err)
	}
	if out.Version, err = r.readString(); err != nil {
		return out, fmt.Errorf("version: %w", err)
	}
	if err = r.readFixed(out.ChainID[:]); err != nil {
		return out, fmt.Errorf("chainId: %w", err)
	}
	if err = r.readFixed(out.Nonce[:]); err != nil {
		return out, fmt.Errorf("nonce: %w", err)
	}
	if err = r.readFixed(out.Expiry[:]); err != nil {
		return out, fmt.Errorf("expiry: %w", err)
	}
	if out.Symbol, err = r.readString(); err != nil {
		return out, fmt.Errorf("symbol: %w", err)
	}
	if err = r.readFixed(out.Price[:]); err != nil {
		return out, fmt.Errorf("price: %w", err)
	}
	if err = r.readFixed(out.Timestamp[:]); err != nil {
		return out, fmt.Errorf("timestamp: %w", err)
	}
	if out.Source, err = r.readString(); err != nil {
		return out, fmt.Errorf("source: %w", err)
	}
	if out.Signature, err = r.readByteVec(); err != nil {
		return out, fmt.Errorf("signature: %w", err)
	}
	if err = r.readFixed(out.Signer[:]); err != nil {
		return out, fmt.Errorf("signer: %w", err)
	}
	if r.remaining() != 0 {
		return out, fmt.Errorf("trailing bytes: %d", r.remaining())
	}
	return out, nil
}

// ---- Borsh primitive helpers ----

type borshWriter struct{ buf []byte }

func (w *borshWriter) writeBytes(b []byte) { w.buf = append(w.buf, b...) }

func (w *borshWriter) writeString(s string) {
	var lenBytes [4]byte
	binary.LittleEndian.PutUint32(lenBytes[:], uint32(len(s)))
	w.buf = append(w.buf, lenBytes[:]...)
	w.buf = append(w.buf, s...)
}

func (w *borshWriter) writeByteVec(b []byte) error {
	var lenBytes [4]byte
	binary.LittleEndian.PutUint32(lenBytes[:], uint32(len(b)))
	w.buf = append(w.buf, lenBytes[:]...)
	w.buf = append(w.buf, b...)
	return nil
}

type borshReader struct {
	buf []byte
	off int
}

func (r *borshReader) remaining() int { return len(r.buf) - r.off }

func (r *borshReader) readU32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, errors.New("short read u32")
	}
	v := binary.LittleEndian.Uint32(r.buf[r.off : r.off+4])
	r.off += 4
	return v, nil
}

func (r *borshReader) readFixed(dst []byte) error {
	if r.remaining() < len(dst) {
		return fmt.Errorf("short read %d bytes", len(dst))
	}
	copy(dst, r.buf[r.off:r.off+len(dst)])
	r.off += len(dst)
	return nil
}

func (r *borshReader) readString() (string, error) {
	n, err := r.readU32()
	if err != nil {
		return "", err
	}
	if r.remaining() < int(n) {
		return "", fmt.Errorf("short read string of len %d", n)
	}
	s := string(r.buf[r.off : r.off+int(n)])
	r.off += int(n)
	return s, nil
}

func (r *borshReader) readByteVec() ([]byte, error) {
	n, err := r.readU32()
	if err != nil {
		return nil, err
	}
	if r.remaining() < int(n) {
		return nil, fmt.Errorf("short read vec<u8> of len %d", n)
	}
	out := make([]byte, n)
	copy(out, r.buf[r.off:r.off+int(n)])
	r.off += int(n)
	return out, nil
}
