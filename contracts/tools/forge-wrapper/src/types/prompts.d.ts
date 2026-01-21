declare module "prompts" {
  export interface Choice<T = any> {
    title: string;
    value: T;
    description?: string;
  }

  export interface PromptObject<T = any> {
    type?:
      | string
      | null
      | ((prev: any, values: any, prompt: PromptObject<T>) => string | null);
    name: string | number | symbol;
    message?: string | ((prev: any, values: any, prompt: PromptObject<T>) => string);
    initial?: any;
    choices?: Choice[];
    separator?: string;
    validate?: (value: any) => boolean | string;
  }

  export interface PromptOptions {
    onSubmit?: (prompt: PromptObject, answer: any, answers: any) => void;
    onCancel?: (prompt: PromptObject) => void;
  }

  export default function prompts<T = any>(
    questions: PromptObject | PromptObject[],
    options?: PromptOptions
  ): Promise<T>;
}
