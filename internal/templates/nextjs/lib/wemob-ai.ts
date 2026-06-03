// Supported model IDs are kept in sync with
// internal/constants/ai_gateway.go:GatewayModelCatalog.
// When adding or removing a gateway model, update both this file and
// that catalog.
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
 *     model: 'anthropic/claude-sonnet-4.6',
 *     messages: [{ role: 'user', content: 'hi' }],
 *   });
 */
export function wemobAI(): OpenAI {
  if (typeof window !== 'undefined') {
    throw new Error('[wemob-ai] server-side only: do not import from client components');
  }
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
  | 'openai/gpt-5.4'
  | 'openai/gpt-5.4-mini'
  | 'openai/gpt-5.4-nano'
  | 'openai/gpt-5.4-pro'
  | 'anthropic/claude-sonnet-4.6'
  | 'anthropic/claude-haiku-4-5'
  | 'anthropic/claude-opus-4.8'
  | 'anthropic/claude-opus-4.8-fast'
  | 'openai/gpt-4.1'
  | 'openai/gpt-4.1-mini'
  | 'google/gemini-2.5-pro'
  | 'google/gemini-3.1-pro-preview'
  | 'google/gemini-3.5-flash'
  | 'google/gemini-3.1-flash-lite'
  | 'x-ai/grok-4.3'
  | 'x-ai/grok-build-0.1'
  | 'qwen/qwen3.7-plus'
  | 'qwen/qwen3.7-max'
  | 'minimax/minimax-m3'
  | 'inception/mercury-2'
  | 'deepseek/deepseek-r1'
  | 'stepfun/step-3.5-flash'
  | 'stepfun/step-3.7-flash';

export type WemobImageModel =
  | 'fal-ai/flux/schnell'
  | 'fal-ai/flux-pro/v1.1'
  | 'fal-ai/flux-pro/v1.1-ultra'
  | 'fal-ai/flux-2-pro'
  | 'fal-ai/flux-pro/kontext'
  | 'fal-ai/nano-banana-2'
  | 'fal-ai/nano-banana-2/edit'
  | 'fal-ai/nano-banana-pro'
  | 'fal-ai/nano-banana-pro/edit'
  | 'fal-ai/gpt-image-2'
  | 'fal-ai/gpt-image-2/image-to-image'
  | 'fal-ai/bytedance/seedream/v4.5/edit'
  | 'xai/grok-imagine-image'
  | 'xai/grok-imagine-image/edit';

export type WemobImageOptions = {
  model?: WemobImageModel;
  prompt: string;
  imageSize?: string;
  numImages?: number;
  imageUrl?: string;
  imageUrls?: string[];
  maskImageUrl?: string;
  quality?: string;
  outputFormat?: string;
  aspectRatio?: string;
  resolution?: string;
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
  if (typeof window !== 'undefined') {
    throw new Error('[wemob-ai] server-side only: do not call from client components');
  }
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
      model: opts.model ?? 'fal-ai/nano-banana-2',
      prompt: opts.prompt,
      image_size: opts.imageSize,
      num_images: opts.numImages,
      image_url: opts.imageUrl,
      image_urls: opts.imageUrls,
      mask_image_url: opts.maskImageUrl,
      quality: opts.quality,
      output_format: opts.outputFormat,
      aspect_ratio: opts.aspectRatio,
      resolution: opts.resolution,
    }),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    let message = text;
    try {
      const parsed = JSON.parse(text) as { error?: { message?: string; code?: string } };
      message = parsed.error?.message ?? parsed.error?.code ?? text;
    } catch {
      // Keep the raw text when the gateway/proxy returns a non-JSON body.
    }
    throw new Error(`image generation failed: ${res.status} ${message}`);
  }
  const data = (await res.json()) as { images?: { url: string }[] };
  return (data.images ?? []).map((i) => i.url);
}
