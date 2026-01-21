import { promises as fs } from "fs";
import path from "path";
import { parse, stringify } from "yaml";

export async function ensureDir(dirPath: string): Promise<void> {
  await fs.mkdir(dirPath, { recursive: true, mode: 0o700 });
}

export async function pathExists(targetPath: string): Promise<boolean> {
  try {
    await fs.access(targetPath);
    return true;
  } catch {
    return false;
  }
}

export async function readYamlFile<T>(filePath: string, fallback: T): Promise<T> {
  try {
    const raw = await fs.readFile(filePath, "utf8");
    return parse(raw) as T;
  } catch (err: any) {
    if (err && err.code === "ENOENT") {
      return fallback;
    }
    throw new Error(`Failed to read YAML from ${filePath}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

export async function writeYamlFile(filePath: string, data: unknown): Promise<void> {
  const dir = path.dirname(filePath);
  await ensureDir(dir);
  const serialized = stringify(data, { indent: 2 });
  await fs.writeFile(filePath, serialized, { encoding: "utf8", mode: 0o600 });
}

export async function readTextFile(filePath: string): Promise<string> {
  const raw = await fs.readFile(filePath, "utf8");
  return raw.trim();
}

export async function writeTextFileSecure(filePath: string, content: string, overwrite = false): Promise<void> {
  const dir = path.dirname(filePath);
  await ensureDir(dir);
  const flags = overwrite ? undefined : "wx";
  try {
    await fs.writeFile(filePath, content, { encoding: "utf8", mode: 0o600, flag: flags });
  } catch (err: any) {
    if (!overwrite && err && err.code === "EEXIST") {
      throw new Error(`File already exists at ${filePath}`);
    }
    throw new Error(`Failed to write secret file ${filePath}: ${err instanceof Error ? err.message : String(err)}`);
  }
}
