const apiKey = process.env.WEMOB_AI_KEY;
const baseURL = process.env.NEXT_PUBLIC_WEMOB_GATEWAY_URL;

function requireConfig(): { apiKey: string; baseURL: string } {
  if (!apiKey || !baseURL) {
    throw new Error(
      'WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL must be set'
    );
  }
  return { apiKey, baseURL };
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

type ChatCompletionResponse = {
  choices?: { message?: { content?: string } }[];
};

type ChatCompletionChunk = {
  choices?: { delta?: { content?: string } }[];
};

export async function chat(opts: WemobChatOptions): Promise<string> {
  const { apiKey, baseURL } = requireConfig();
  const res = await fetch(`${baseURL}/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify({
      model: opts.model ?? 'claude-sonnet-4',
      messages: opts.messages,
      temperature: opts.temperature,
      max_tokens: opts.maxTokens,
    }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`chat failed: ${res.status} ${text}`);
  }
  const data = (await res.json()) as ChatCompletionResponse;
  return data.choices?.[0]?.message?.content ?? '';
}

export async function chatStream(
  opts: WemobChatOptions
): Promise<ReadableStream<Uint8Array>> {
  const { apiKey, baseURL } = requireConfig();
  const upstream = await fetch(`${baseURL}/chat/completions`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${apiKey}`,
    },
    body: JSON.stringify({
      model: opts.model ?? 'claude-sonnet-4',
      messages: opts.messages,
      temperature: opts.temperature,
      max_tokens: opts.maxTokens,
      stream: true,
    }),
  });

  if (!upstream.ok || !upstream.body) {
    const text = await upstream.text().catch(() => '');
    throw new Error(`chat stream failed: ${upstream.status} ${text}`);
  }

  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  const reader = upstream.body.getReader();

  return new ReadableStream({
    async start(controller) {
      let buffer = '';
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });

          let idx: number;
          while ((idx = buffer.indexOf('\n')) !== -1) {
            const line = buffer.slice(0, idx).trim();
            buffer = buffer.slice(idx + 1);
            if (!line.startsWith('data:')) continue;
            const data = line.slice(5).trim();
            if (data === '[DONE]') {
              controller.close();
              return;
            }
            try {
              const chunk = JSON.parse(data) as ChatCompletionChunk;
              const delta = chunk.choices?.[0]?.delta?.content;
              if (delta) controller.enqueue(encoder.encode(delta));
            } catch {
              // Skip malformed lines silently — gateway sometimes sends keep-alives.
            }
          }
        }
        controller.close();
      } catch (err) {
        controller.error(err);
      }
    },
    cancel() {
      reader.cancel().catch(() => {});
    },
  });
}

export type WemobImageOptions = {
  model?: WemobImageModel;
  prompt: string;
  width?: number;
  height?: number;
};

type ImageResponse = {
  images?: { url: string }[];
};

export async function generateImage(
  opts: WemobImageOptions
): Promise<string[]> {
  const { apiKey, baseURL } = requireConfig();
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
    const text = await res.text().catch(() => '');
    throw new Error(`image generation failed: ${res.status} ${text}`);
  }
  const data = (await res.json()) as ImageResponse;
  return (data.images ?? []).map((i) => i.url);
}
