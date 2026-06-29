package arch

// OracleInstruction variant discriminators. Order matches dia_arch_shared::instruction::OracleInstruction
// declaration order; Borsh enum encoding uses the variant index as a single leading u8.
const (
	OracleInstructionInitialize              uint8 = 0
	OracleInstructionHandleIntentUpdate      uint8 = 1
	OracleInstructionHandleBatchIntentUpdates uint8 = 2
	OracleInstructionSetSignerAuthorization  uint8 = 3
	OracleInstructionSetDomainSeparator      uint8 = 4
	OracleInstructionSetPaymentHook          uint8 = 5
	OracleInstructionTransferOwnership       uint8 = 6
	OracleInstructionRecoverLamports         uint8 = 7
)

// BuildHandleIntentUpdateData Borsh-encodes
//   OracleInstruction::HandleIntentUpdate { intent }
// as a single byte stream suitable for an Instruction.Data field.
func BuildHandleIntentUpdateData(intent OracleIntent) ([]byte, error) {
	body, err := MarshalOracleIntent(intent)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(body))
	out = append(out, OracleInstructionHandleIntentUpdate)
	out = append(out, body...)
	return out, nil
}
