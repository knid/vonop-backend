import OpenAI from 'openai';

let _client: OpenAI | null = null;

/**
 * Returns a preconfigured OpenAI client pointed at the Wemob AI Gateway.
 * Use it exactly like the real OpenAI SDK — chat, streaming, tools,
 * tool_choice, response_format, seed, and vision all work through the
 * gateway without any extra wrapping.
 *
 * Server-side only: import this from API routes, Server Actions, or
 * Server Components. Never from a "use client" file — WEMOB_AI_KEY must
 * never reach the browser.
 *
 * Example:
 *   import { wemobAI } from '@/lib/wemob-ai';
 *   const res = await wemobAI().chat.completions.create({
 *     model: 'anthropic/claude-sonnet-4',
 *     messages: [{ role: 'user', content: 'hi' }],
 *   });
 */
export function wemobAI(): OpenAI {
  if (_client) return _client;
  const apiKey = process.env.WEMOB_AI_KEY;
  const baseURL = process.env.NEXT_PUBLIC_WEMOB_GATEWAY_URL;
  if (!apiKey || !baseURL) {
    throw new Error(
      '[wemob-ai] WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL must be set'
    );
  }
  _client = new OpenAI({ apiKey, baseURL });
  return _client;
}

/**
 * Supported chat model IDs. Pass any of these as the `model` field on
 * wemobAI().chat.completions.create({ ... }).
 */
export type WemobChatModel =
  | 'anthropic/claude-sonnet-4'
  | 'anthropic/claude-haiku-4-5'
  | 'anthropic/claude-opus-4'
  | 'openai/gpt-4.1'
  | 'openai/gpt-4.1-mini'
  | 'google/gemini-2.5-pro';

export type WemobImageModel =
  | 'fal-ai/flux-schnell'
  | 'fal-ai/flux-pro/v1.1'
  | 'fal-ai/flux-ultra';

export type WemobImageOptions = {
  model?: WemobImageModel;
  prompt: string;
  imageSize?: string;
  numImages?: number;
};

/**
 * Generate images via the Wemob AI Gateway. Image generation is proxied
 * to fal.ai, which doesn't speak OpenAI's Images API shape — hence this
 * thin wrapper instead of wemobAI().images.generate().
 *
 * Returns an array of public image URLs.
 */
export async function generateImage(
  opts: WemobImageOptions
): Promise<string[]> {
  const apiKey = process.env.WEMOB_AI_KEY;
  const baseURL = process.env.NEXT_PUBLIC_WEMOB_GATEWAY_URL;
  if (!apiKey || !baseURL) {
    throw new Error(
      '[wemob-ai] WEMOB_AI_KEY and NEXT_PUBLIC_WEMOB_GATEWAY_URL must be set'
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
      image_size: opts.imageSize,
      num_images: opts.numImages,
    }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`image generation failed: ${res.status} ${text}`);
  }
  const data = (await res.json()) as { images?: { url: string }[] };
  return (data.images ?? []).map((i) => i.url);
}
