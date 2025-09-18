import { spawn } from "child_process";
import chalk from "chalk";
import { ForgeExecution } from "../types";

export interface CommandOptions {
  env?: Record<string, string>;
  cwd?: string;
  echoCommand?: boolean;
}

export function formatCommand(binary: string, args: string[]): string {
  return [binary, ...args.map((arg) => (arg.includes(" ") ? `'${arg}'` : arg))].join(" ");
}

export async function runBinary(
  binary: string,
  args: string[],
  options: CommandOptions = {}
): Promise<ForgeExecution> {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, {
      cwd: options.cwd ?? process.cwd(),
      env: { ...process.env, ...options.env },
      stdio: ["ignore", "pipe", "pipe"],
    });

    if (options.echoCommand) {
      const cmd = formatCommand(binary, args);
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`$ ${cmd}`));
    }

    let stdout = "";
    let stderr = "";

    child.stdout?.on("data", (chunk) => {
      stdout += chunk.toString();
    });

    child.stderr?.on("data", (chunk) => {
      stderr += chunk.toString();
    });

    child.on("error", (error) => {
      reject(error);
    });

    child.on("close", (code) => {
      if (code === 0) {
        resolve({ stdout, stderr });
        return;
      }

      const error = new Error(`${binary} exited with code ${code}`);
      (error as any).code = code;
      (error as any).stdout = stdout;
      (error as any).stderr = stderr;
      reject(error);
    });
  });
}

export async function runForge(args: string[], options: CommandOptions = {}): Promise<ForgeExecution> {
  return runBinary("forge", args, options);
}

export async function runCast(args: string[], options: CommandOptions = {}): Promise<ForgeExecution> {
  return runBinary("cast", args, options);
}
