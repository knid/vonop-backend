import OpenAI from 'openai';

const apiKey = process.env.WEMOB_AI_KEY;
const baseURL = process.env.NEXT_PUBLIC_WEMOB_GATEWAY_URL;

let _client: OpenAI | null = null;

function getClient(): OpenAI {
  if (!_client) {
    if (!apiKey || !baseURL) {
      throw new Error(
        'WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL must be set'
      );
    }
    _client = new OpenAI({ apiKey, baseURL });
  }
  return _client;
}

export type WemobChatModel =
  | 'claude-sonnet-4'
  | 'claude-haiku-4-5'
  | 'claude-opus-4'
  | 'gpt-4.1'
  | 'gpt-4.1-mini'
  | 'gemini-2.5-pro';

export type WemobImageModel =
  | 'fal-ai/flux-schnell'
  | 'fal-ai/flux-pro/v1.1'
  | 'fal-ai/flux-ultra';

export type WemobMessage = {
  role: 'system' | 'user' | 'assistant';
  content: string;
};

export type WemobChatOptions = {
  model?: WemobChatModel;
  messages: WemobMessage[];
  temperature?: number;
  maxTokens?: number;
};

export async function chat(opts: WemobChatOptions): Promise<string> {
  const client = getClient();
  const res = await client.chat.completions.create({
    model: opts.model ?? 'claude-sonnet-4',
    messages: opts.messages,
    temperature: opts.temperature,
    max_tokens: opts.maxTokens,
  });
  return res.choices[0]?.message?.content ?? '';
}

export async function chatStream(
  opts: WemobChatOptions
): Promise<ReadableStream<Uint8Array>> {
  const client = getClient();
  const upstream = await client.chat.completions.create({
    model: opts.model ?? 'claude-sonnet-4',
    messages: opts.messages,
    temperature: opts.temperature,
    max_tokens: opts.maxTokens,
    stream: true,
  });

  const encoder = new TextEncoder();
  return new ReadableStream({
    async start(controller) {
      try {
        for await (const chunk of upstream) {
          const delta = chunk.choices[0]?.delta?.content;
          if (delta) controller.enqueue(encoder.encode(delta));
        }
        controller.close();
      } catch (err) {
        controller.error(err);
      }
    },
  });
}

export type WemobImageOptions = {
  model?: WemobImageModel;
  prompt: string;
  width?: number;
  height?: number;
};

export async function generateImage(
  opts: WemobImageOptions
): Promise<string[]> {
  if (!apiKey || !baseURL) {
    throw new Error(
      'WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL must be set'
    );
  }
  const res = await fetch(`${baseURL}/images/generate`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify({
      model: opts.model ?? 'fal-ai/flux-schnell',
      prompt: opts.prompt,
      width: opts.width,
      height: opts.height,
    }),
  });
  if (!res.ok) {
    throw new Error(`image generation failed: ${res.status}`);
  }
  const data = (await res.json()) as { images?: { url: string }[] };
  return (data.images ?? []).map((i) => i.url);
}
