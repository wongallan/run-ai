# GitHub Copilot Provider Implementation Specification

This specification provides comprehensive details for implementing GitHub Copilot integration based on the OpenCode implementation. It covers authentication, model management, request handling, and all necessary technical details. This implementation must support both GitHub.com and GitHub Enterprise deployments.

## Table of Contents
1. [Authentication](#authentication)
2. [Provider Configuration](#provider-configuration)
3. [Model List & Selection](#model-list--selection)
4. [API Selection (Chat vs Responses)](#api-selection-chat-vs-responses)
5. [Request Construction](#request-construction)
6. [Response Handling](#response-handling)
7. [Premium Features & Subscription Handling](#premium-features--subscription-handling)
8. [Enterprise Support](#enterprise-support)

---

## 1. Authentication

### OAuth Device Flow

GitHub Copilot uses the OAuth 2.0 Device Authorization Grant flow for authentication.

**Client Configuration:**
- Client ID: `Ov23li8tweQw6odWQebz`
- Scope: `read:user`

**Authentication Flow:**

1. **Initiate Device Code Request:**
   - POST to `https://{domain}/login/device/code`
   - Headers: `Accept: application/json`, `Content-Type: application/json`, `User-Agent: opencode/{version}`
  - Body: `{ "client_id": "{CLIENT_ID}", "scope": "read:user" }`

2. **Display User Code:**
   - Response contains: `verification_uri`, `user_code`, `device_code`, `interval`
  - Direct user to `verification_uri` and show them `user_code`

3. **Poll for Access Token:**
   - POST to `https://{domain}/login/oauth/access_token`
   - Body: `{ "client_id": "{CLIENT_ID}", "device_code": "{device_code}", "grant_type": "urn:ietf:params:oauth:grant-type:device_code" }`
   - Poll every `interval` seconds + 3 second safety margin
  - Handle errors: `authorization_pending` (continue), `slow_down` (increase interval by 5s or use server-provided interval)

4. **Store Tokens:**
   - Store `access_token` as both the refresh and access token
  - For enterprise deployments, also store the `enterpriseUrl`

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/plugin/copilot.ts (L5-7)
```typescript
const CLIENT_ID = "Ov23li8tweQw6odWQebz"
// Add a small safety buffer when polling to avoid hitting the server
// slightly too early due to clock skew / timer drift.
```

**File:** packages/opencode/src/plugin/copilot.ts (L197-208)
```typescript
            const deviceResponse = await fetch(urls.DEVICE_CODE_URL, {
              method: "POST",
              headers: {
                Accept: "application/json",
                "Content-Type": "application/json",
                "User-Agent": `opencode/${Installation.VERSION}`,
              },
              body: JSON.stringify({
                client_id: CLIENT_ID,
                scope: "read:user",
              }),
            })
```

**File:** packages/opencode/src/plugin/copilot.ts (L214-223)
```typescript
            const deviceData = (await deviceResponse.json()) as {
              verification_uri: string
              user_code: string
              device_code: string
              interval: number
            }

            return {
              url: deviceData.verification_uri,
              instructions: `Enter code: ${deviceData.user_code}`,
```

**File:** packages/opencode/src/plugin/copilot.ts (L226-297)
```typescript
                while (true) {
                  const response = await fetch(urls.ACCESS_TOKEN_URL, {
                    method: "POST",
                    headers: {
                      Accept: "application/json",
                      "Content-Type": "application/json",
                      "User-Agent": `opencode/${Installation.VERSION}`,
                    },
                    body: JSON.stringify({
                      client_id: CLIENT_ID,
                      device_code: deviceData.device_code,
                      grant_type: "urn:ietf:params:oauth:grant-type:device_code",
                    }),
                  })

                  if (!response.ok) return { type: "failed" as const }

                  const data = (await response.json()) as {
                    access_token?: string
                    error?: string
                    interval?: number
                  }

                  if (data.access_token) {
                    const result: {
                      type: "success"
                      refresh: string
                      access: string
                      expires: number
                      provider?: string
                      enterpriseUrl?: string
                    } = {
                      type: "success",
                      refresh: data.access_token,
                      access: data.access_token,
                      expires: 0,
                    }

                    if (actualProvider === "github-copilot-enterprise") {
                      result.provider = "github-copilot-enterprise"
                      result.enterpriseUrl = domain
                    }

                    return result
                  }

                  if (data.error === "authorization_pending") {
                    await Bun.sleep(deviceData.interval * 1000 + OAUTH_POLLING_SAFETY_MARGIN_MS)
                    continue
                  }

                  if (data.error === "slow_down") {
                    // Based on the RFC spec, we must add 5 seconds to our current polling interval.
                    // (See https://www.rfc-editor.org/rfc/rfc8628#section-3.5)
                    let newInterval = (deviceData.interval + 5) * 1000

                    // GitHub OAuth API may return the new interval in seconds in the response.
                    // We should try to use that if provided with safety margin.
                    const serverInterval = data.interval
                    if (serverInterval && typeof serverInterval === "number" && serverInterval > 0) {
                      newInterval = serverInterval * 1000
                    }

                    await Bun.sleep(newInterval + OAUTH_POLLING_SAFETY_MARGIN_MS)
                    continue
                  }

                  if (data.error) return { type: "failed" as const }

                  await Bun.sleep(deviceData.interval * 1000 + OAUTH_POLLING_SAFETY_MARGIN_MS)
                  continue
                }
```

---

## 2. Provider Configuration

### Base URL Construction

**For GitHub.com (Standard):**
- Base URL: Default OpenAI-compatible endpoint (typically injected via SDK)

**For GitHub Enterprise:**
- Base URL: `https://copilot-api.{normalized-enterprise-domain}`
- Example: If enterprise URL is `https://company.ghe.com`, base URL becomes `https://copilot-api.company.ghe.com`

### Custom Fetch Interceptor

All API requests must inject authentication and metadata headers:

**Required Headers:**
- `Authorization: Bearer {access_token}`
- `User-Agent: opencode/{version}` (or your application identifier)
- `Openai-Intent: conversation-edits`
- `x-initiator: user` or `agent` (based on whether the last message is from user or assistant/tool)

**Conditional Headers:**
- `Copilot-Vision-Request: true` - When the request contains image content
- `anthropic-beta: interleaved-thinking-2025-05-14` - For Claude models accessed through Copilot

**x-initiator Logic:**

For Chat/Completions API:
- Check if last message role is `user` - set `user`, otherwise `agent`

For Responses API:
- Check if last input item role is `user` - set `user`, otherwise `agent`

For Messages API (Anthropic):
- Set `user` if last message is user role with non-tool-result content, otherwise `agent`

**Vision Detection:**

Detect vision requests by checking for:
- Chat API: messages with `image_url` content parts
- Responses API: input items with `input_image` type
- Messages API: content with `image` type or nested images in `tool_result` content

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/plugin/copilot.ts (L29-30)
```typescript
        const enterpriseUrl = info.enterpriseUrl
        const baseURL = enterpriseUrl ? `https://copilot-api.${normalizeDomain(enterpriseUrl)}` : undefined
```

**File:** packages/opencode/src/plugin/copilot.ts (L68-119)
```typescript
            const { isVision, isAgent } = iife(() => {
              try {
                const body = typeof init?.body === "string" ? JSON.parse(init.body) : init?.body

                // Completions API
                if (body?.messages && url.includes("completions")) {
                  const last = body.messages[body.messages.length - 1]
                  return {
                    isVision: body.messages.some(
                      (msg: any) =>
                        Array.isArray(msg.content) && msg.content.some((part: any) => part.type === "image_url"),
                    ),
                    isAgent: last?.role !== "user",
                  }
                }

                // Responses API
                if (body?.input) {
                  const last = body.input[body.input.length - 1]
                  return {
                    isVision: body.input.some(
                      (item: any) =>
                        Array.isArray(item?.content) && item.content.some((part: any) => part.type === "input_image"),
                    ),
                    isAgent: last?.role !== "user",
                  }
                }

                // Messages API
                if (body?.messages) {
                  const last = body.messages[body.messages.length - 1]
                  const hasNonToolCalls =
                    Array.isArray(last?.content) && last.content.some((part: any) => part?.type !== "tool_result")
                  return {
                    isVision: body.messages.some(
                      (item: any) =>
                        Array.isArray(item?.content) &&
                        item.content.some(
                          (part: any) =>
                            part?.type === "image" ||
                            // images can be nested inside tool_result content
                            (part?.type === "tool_result" &&
                              Array.isArray(part?.content) &&
                              part.content.some((nested: any) => nested?.type === "image")),
                        ),
                    ),
                    isAgent: !(last?.role === "user" && hasNonToolCalls),
                  }
                }
              } catch {}
              return { isVision: false, isAgent: false }
            })
```

**File:** packages/opencode/src/plugin/copilot.ts (L121-139)
```typescript
            const headers: Record<string, string> = {
              "x-initiator": isAgent ? "agent" : "user",
              ...(init?.headers as Record<string, string>),
              "User-Agent": `opencode/${Installation.VERSION}`,
              Authorization: `Bearer ${info.refresh}`,
              "Openai-Intent": "conversation-edits",
            }

            if (isVision) {
              headers["Copilot-Vision-Request"] = "true"
            }

            delete headers["x-api-key"]
            delete headers["authorization"]

            return fetch(request, {
              ...init,
              headers,
            })
```

---

## 3. Model List & Selection

### Obtaining Model List

Models should be sourced from a centralized model registry (like models.dev in OpenCode's case). The GitHub Copilot provider doesn't expose its own model listing API - you need to maintain a static or regularly updated list of supported models.

### Provider Registry

Two provider IDs are supported:
- `github-copilot` - Standard GitHub.com Copilot
- `github-copilot-enterprise` - Enterprise/Data Residency deployments

The enterprise provider inherits all models from the standard provider but uses a different base URL and authentication.

### Model Examples

Common models available through Copilot:
- GPT-5 series: `gpt-5`, `gpt-5-mini`, `gpt-5.2`, `gpt-5.2-codex`, `gpt-5-nano`
- Claude models: `claude-sonnet-4-5`, `claude-haiku-4-5`
- Gemini models: `gemini-3-flash-preview`
- O-series: `o3`, `o4-mini`

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/provider.ts (L723-735)
```typescript
    // Add GitHub Copilot Enterprise provider that inherits from GitHub Copilot
    if (database["github-copilot"]) {
      const githubCopilot = database["github-copilot"]
      database["github-copilot-enterprise"] = {
        ...githubCopilot,
        id: "github-copilot-enterprise",
        name: "GitHub Copilot Enterprise",
        models: mapValues(githubCopilot.models, (model) => ({
          ...model,
          providerID: "github-copilot-enterprise",
        })),
      }
    }
```

---

## 4. API Selection (Chat vs Responses)

GitHub Copilot supports two different API endpoints, and the selection depends on the model:

### Selection Logic

**Use Responses API when:**
- Model ID matches `gpt-5` or later (GPT-5, GPT-6, etc.)
- AND model is NOT `gpt-5-mini`

**Use Chat API for:**
- All other models including `gpt-5-mini`, Claude, Gemini, O-series

### Custom Loader Implementation

The provider must implement a custom model loader that returns the appropriate SDK method.

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/provider.ts (L46-56)
```typescript
  function isGpt5OrLater(modelID: string): boolean {
    const match = /^gpt-(\d+)/.exec(modelID)
    if (!match) {
      return false
    }
    return Number(match[1]) >= 5
  }

  function shouldUseCopilotResponsesApi(modelID: string): boolean {
    return isGpt5OrLater(modelID) && !modelID.startsWith("gpt-5-mini")
  }
```

**File:** packages/opencode/src/provider/provider.ts (L133-142)
```typescript
    "github-copilot": async () => {
      return {
        autoload: false,
        async getModel(sdk: any, modelID: string, _options?: Record<string, any>) {
          if (sdk.responses === undefined && sdk.chat === undefined) return sdk.languageModel(modelID)
          return shouldUseCopilotResponsesApi(modelID) ? sdk.responses(modelID) : sdk.chat(modelID)
        },
        options: {},
      }
    },
```

---

## 5. Request Construction

### 5.1 Chat API (OpenAI-Compatible)

**Endpoint:** `POST {baseURL}/chat/completions`

**Request Body Structure:**
```typescript
{
  model: string,
  messages: Array<Message>,
  max_tokens?: number,
  temperature?: number,
  top_p?: number,
  frequency_penalty?: number,
  presence_penalty?: number,
  stop?: string[],
  seed?: number,
  tools?: Array<Tool>,
  tool_choice?: ToolChoice,
  response_format?: ResponseFormat,
  reasoning_effort?: "low" | "medium" | "high" | "xhigh",
  verbosity?: "low" | "medium" | "high",
  thinking_budget?: number,
  user?: string,
  stream?: boolean
}
```

**Message Format:**

Messages follow OpenAI's chat format with Copilot-specific extensions.

**Copilot-Specific Fields:**

Assistant messages can include:
- `reasoning_text`: The reasoning content (visible)
- `reasoning_opaque`: Encrypted reasoning state for multi-turn reasoning continuation

**Message Conversion:**

Convert from AI SDK v2 format to Copilot chat messages:
- Handle `reasoning_opaque` from provider metadata to enable multi-turn reasoning
- Attach reasoning text to assistant messages when present

### 5.2 Responses API (OpenAI Responses)

**Endpoint:** `POST {baseURL}/responses`

**Request Body Structure:**
```typescript
{
  model: string,
  input: Array<InputItem>,
  max_output_tokens?: number,
  temperature?: number,
  top_p?: number,
  text?: {
    format?: { type: "json_object" } | { type: "json_schema", schema, name, description },
    verbosity?: "low" | "medium" | "high"
  },
  reasoning?: {
    effort?: "none" | "minimal" | "low" | "medium" | "high" | "xhigh",
    summary?: "auto" | "enabled" | "disabled"
  },
  tools?: Array<Tool>,
  tool_choice?: ToolChoice,
  max_tool_calls?: number,
  parallel_tool_calls?: boolean,
  metadata?: object,
  previous_response_id?: string,
  store?: boolean,
  user?: string,
  instructions?: string,
  service_tier?: "auto" | "default" | "flex" | "priority",
  include?: Array<string>,
  prompt_cache_key?: string,
  safety_identifier?: string,
  top_logprobs?: number,
  truncation?: "auto",
  stream?: boolean
}
```

**Input Item Types:**

The Responses API uses a different message format with `input` array containing various item types:
- `{ role: "system" | "developer" | "user" | "assistant", content: ... }`
- `{ type: "function_call", call_id, name, arguments, id }`
- `{ type: "function_call_output", call_id, output }`
- `{ type: "reasoning", id, encrypted_content, summary }`
- `{ type: "item_reference", id }` - Reference to stored items when `store: true`
- `{ type: "local_shell_call", ... }` - For local shell tool execution
- Multiple other provider-specific types

**Input Content Parts:**

User input supports:
- `{ type: "input_text", text: string }`
- `{ type: "input_image", image_url: string | file_id: string, detail?: string }`
- `{ type: "input_file", file_url: string | file_id: string | file_data: string, filename?: string }`

Assistant content supports:
- `{ type: "output_text", text: string }`

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L140-189)
```typescript
      args: {
        // model id:
        model: this.modelId,

        // model specific settings:
        user: compatibleOptions.user,

        // standardized settings:
        max_tokens: maxOutputTokens,
        temperature,
        top_p: topP,
        frequency_penalty: frequencyPenalty,
        presence_penalty: presencePenalty,
        response_format:
          responseFormat?.type === "json"
            ? this.supportsStructuredOutputs === true && responseFormat.schema != null
              ? {
                  type: "json_schema",
                  json_schema: {
                    schema: responseFormat.schema,
                    name: responseFormat.name ?? "response",
                    description: responseFormat.description,
                  },
                }
              : { type: "json_object" }
            : undefined,

        stop: stopSequences,
        seed,
        ...Object.fromEntries(
          Object.entries(providerOptions?.[this.providerOptionsName] ?? {}).filter(
            ([key]) => !Object.keys(openaiCompatibleProviderOptions.shape).includes(key),
          ),
        ),

        reasoning_effort: compatibleOptions.reasoningEffort,
        verbosity: compatibleOptions.textVerbosity,

        // messages:
        messages: convertToOpenAICompatibleChatMessages(prompt),

        // tools:
        tools: openaiTools,
        tool_choice: openaiToolChoice,

        // thinking_budget
        thinking_budget: compatibleOptions.thinking_budget,
      },
      warnings: [...warnings, ...toolWarnings],
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-api-types.ts (L1-64)
```typescript
import type { JSONValue } from "@ai-sdk/provider"

export type OpenAICompatibleChatPrompt = Array<OpenAICompatibleMessage>

export type OpenAICompatibleMessage =
  | OpenAICompatibleSystemMessage
  | OpenAICompatibleUserMessage
  | OpenAICompatibleAssistantMessage
  | OpenAICompatibleToolMessage

// Allow for arbitrary additional properties for general purpose
// provider-metadata-specific extensibility.
type JsonRecord<T = never> = Record<string, JSONValue | JSONValue[] | T | T[] | undefined>

export interface OpenAICompatibleSystemMessage extends JsonRecord<OpenAICompatibleSystemContentPart> {
  role: "system"
  content: string | Array<OpenAICompatibleSystemContentPart>
}

export interface OpenAICompatibleSystemContentPart extends JsonRecord {
  type: "text"
  text: string
}

export interface OpenAICompatibleUserMessage extends JsonRecord<OpenAICompatibleContentPart> {
  role: "user"
  content: string | Array<OpenAICompatibleContentPart>
}

export type OpenAICompatibleContentPart = OpenAICompatibleContentPartText | OpenAICompatibleContentPartImage

export interface OpenAICompatibleContentPartImage extends JsonRecord {
  type: "image_url"
  image_url: { url: string }
}

export interface OpenAICompatibleContentPartText extends JsonRecord {
  type: "text"
  text: string
}

export interface OpenAICompatibleAssistantMessage extends JsonRecord<OpenAICompatibleMessageToolCall> {
  role: "assistant"
  content?: string | null
  tool_calls?: Array<OpenAICompatibleMessageToolCall>
  // Copilot-specific reasoning fields
  reasoning_text?: string
  reasoning_opaque?: string
}

export interface OpenAICompatibleMessageToolCall extends JsonRecord {
  type: "function"
  id: string
  function: {
    arguments: string
    name: string
  }
}

export interface OpenAICompatibleToolMessage extends JsonRecord {
  role: "tool"
  content: string
  tool_call_id: string
}
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/convert-to-openai-compatible-chat-messages.ts (L73-124)
```typescript
      case "assistant": {
        let text = ""
        let reasoningText: string | undefined
        let reasoningOpaque: string | undefined
        const toolCalls: Array<{
          id: string
          type: "function"
          function: { name: string; arguments: string }
        }> = []

        for (const part of content) {
          const partMetadata = getOpenAIMetadata(part)
          // Check for reasoningOpaque on any part (may be attached to text/tool-call)
          const partOpaque = (part.providerOptions as { copilot?: { reasoningOpaque?: string } })?.copilot
            ?.reasoningOpaque
          if (partOpaque && !reasoningOpaque) {
            reasoningOpaque = partOpaque
          }

          switch (part.type) {
            case "text": {
              text += part.text
              break
            }
            case "reasoning": {
              if (part.text) reasoningText = part.text
              break
            }
            case "tool-call": {
              toolCalls.push({
                id: part.toolCallId,
                type: "function",
                function: {
                  name: part.toolName,
                  arguments: JSON.stringify(part.input),
                },
                ...partMetadata,
              })
              break
            }
          }
        }

        messages.push({
          role: "assistant",
          content: text || null,
          tool_calls: toolCalls.length > 0 ? toolCalls : undefined,
          reasoning_text: reasoningOpaque ? reasoningText : undefined,
          reasoning_opaque: reasoningOpaque,
          ...metadata,
        })
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L254-310)
```typescript
    const baseArgs = {
      model: this.modelId,
      input,
      temperature,
      top_p: topP,
      max_output_tokens: maxOutputTokens,

      ...((responseFormat?.type === "json" || openaiOptions?.textVerbosity) && {
        text: {
          ...(responseFormat?.type === "json" && {
            format:
              responseFormat.schema != null
                ? {
                    type: "json_schema",
                    strict: strictJsonSchema,
                    name: responseFormat.name ?? "response",
                    description: responseFormat.description,
                    schema: responseFormat.schema,
                  }
                : { type: "json_object" },
          }),
          ...(openaiOptions?.textVerbosity && {
            verbosity: openaiOptions.textVerbosity,
          }),
        },
      }),

      // provider options:
      max_tool_calls: openaiOptions?.maxToolCalls,
      metadata: openaiOptions?.metadata,
      parallel_tool_calls: openaiOptions?.parallelToolCalls,
      previous_response_id: openaiOptions?.previousResponseId,
      store: openaiOptions?.store,
      user: openaiOptions?.user,
      instructions: openaiOptions?.instructions,
      service_tier: openaiOptions?.serviceTier,
      include,
      prompt_cache_key: openaiOptions?.promptCacheKey,
      safety_identifier: openaiOptions?.safetyIdentifier,
      top_logprobs: topLogprobs,

      // model-specific settings:
      ...(modelConfig.isReasoningModel &&
        (openaiOptions?.reasoningEffort != null || openaiOptions?.reasoningSummary != null) && {
          reasoning: {
            ...(openaiOptions?.reasoningEffort != null && {
              effort: openaiOptions.reasoningEffort,
            }),
            ...(openaiOptions?.reasoningSummary != null && {
              summary: openaiOptions.reasoningSummary,
            }),
          },
        }),
      ...(modelConfig.requiredAutoTruncation && {
        truncation: "auto",
      }),
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/convert-to-openai-responses-input.ts (L21-295)
```typescript
export async function convertToOpenAIResponsesInput({
  prompt,
  systemMessageMode,
  fileIdPrefixes,
  store,
  hasLocalShellTool = false,
}: {
  prompt: LanguageModelV2Prompt
  systemMessageMode: "system" | "developer" | "remove"
  fileIdPrefixes?: readonly string[]
  store: boolean
  hasLocalShellTool?: boolean
}): Promise<{
  input: OpenAIResponsesInput
  warnings: Array<LanguageModelV2CallWarning>
}> {
  const input: OpenAIResponsesInput = []
  const warnings: Array<LanguageModelV2CallWarning> = []

  for (const { role, content } of prompt) {
    switch (role) {
      case "system": {
        switch (systemMessageMode) {
          case "system": {
            input.push({ role: "system", content })
            break
          }
          case "developer": {
            input.push({ role: "developer", content })
            break
          }
          case "remove": {
            warnings.push({
              type: "other",
              message: "system messages are removed for this model",
            })
            break
          }
          default: {
            const _exhaustiveCheck: never = systemMessageMode
            throw new Error(`Unsupported system message mode: ${_exhaustiveCheck}`)
          }
        }
        break
      }

      case "user": {
        input.push({
          role: "user",
          content: content.map((part, index) => {
            switch (part.type) {
              case "text": {
                return { type: "input_text", text: part.text }
              }
              case "file": {
                if (part.mediaType.startsWith("image/")) {
                  const mediaType = part.mediaType === "image/*" ? "image/jpeg" : part.mediaType

                  return {
                    type: "input_image",
                    ...(part.data instanceof URL
                      ? { image_url: part.data.toString() }
                      : typeof part.data === "string" && isFileId(part.data, fileIdPrefixes)
                        ? { file_id: part.data }
                        : {
                            image_url: `data:${mediaType};base64,${convertToBase64(part.data)}`,
                          }),
                    detail: part.providerOptions?.openai?.imageDetail,
                  }
                } else if (part.mediaType === "application/pdf") {
                  if (part.data instanceof URL) {
                    return {
                      type: "input_file",
                      file_url: part.data.toString(),
                    }
                  }
                  return {
                    type: "input_file",
                    ...(typeof part.data === "string" && isFileId(part.data, fileIdPrefixes)
                      ? { file_id: part.data }
                      : {
                          filename: part.filename ?? `part-${index}.pdf`,
                          file_data: `data:application/pdf;base64,${convertToBase64(part.data)}`,
                        }),
                  }
                } else {
                  throw new UnsupportedFunctionalityError({
                    functionality: `file part media type ${part.mediaType}`,
                  })
                }
              }
            }
          }),
        })

        break
      }

      case "assistant": {
        const reasoningMessages: Record<string, OpenAIResponsesReasoning> = {}
        const toolCallParts: Record<string, LanguageModelV2ToolCallPart> = {}

        for (const part of content) {
          switch (part.type) {
            case "text": {
              input.push({
                role: "assistant",
                content: [{ type: "output_text", text: part.text }],
                id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
              })
              break
            }
            case "tool-call": {
              toolCallParts[part.toolCallId] = part

              if (part.providerExecuted) {
                break
              }

              if (hasLocalShellTool && part.toolName === "local_shell") {
                const parsedInput = localShellInputSchema.parse(part.input)
                input.push({
                  type: "local_shell_call",
                  call_id: part.toolCallId,
                  id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
                  action: {
                    type: "exec",
                    command: parsedInput.action.command,
                    timeout_ms: parsedInput.action.timeoutMs,
                    user: parsedInput.action.user,
                    working_directory: parsedInput.action.workingDirectory,
                    env: parsedInput.action.env,
                  },
                })

                break
              }

              input.push({
                type: "function_call",
                call_id: part.toolCallId,
                name: part.toolName,
                arguments: JSON.stringify(part.input),
                id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
              })
              break
            }

            // assistant tool result parts are from provider-executed tools:
            case "tool-result": {
              if (store) {
                // use item references to refer to tool results from built-in tools
                input.push({ type: "item_reference", id: part.toolCallId })
              } else {
                warnings.push({
                  type: "other",
                  message: `Results for OpenAI tool ${part.toolName} are not sent to the API when store is false`,
                })
              }

              break
            }

            case "reasoning": {
              const providerOptions = await parseProviderOptions({
                provider: "copilot",
                providerOptions: part.providerOptions,
                schema: openaiResponsesReasoningProviderOptionsSchema,
              })

              const reasoningId = providerOptions?.itemId

              if (reasoningId != null) {
                const reasoningMessage = reasoningMessages[reasoningId]

                if (store) {
                  if (reasoningMessage === undefined) {
                    // use item references to refer to reasoning (single reference)
                    input.push({ type: "item_reference", id: reasoningId })

                    // store unused reasoning message to mark id as used
                    reasoningMessages[reasoningId] = {
                      type: "reasoning",
                      id: reasoningId,
                      summary: [],
                    }
                  }
                } else {
                  const summaryParts: Array<{
                    type: "summary_text"
                    text: string
                  }> = []

                  if (part.text.length > 0) {
                    summaryParts.push({
                      type: "summary_text",
                      text: part.text,
                    })
                  } else if (reasoningMessage !== undefined) {
                    warnings.push({
                      type: "other",
                      message: `Cannot append empty reasoning part to existing reasoning sequence. Skipping reasoning part: ${JSON.stringify(part)}.`,
                    })
                  }

                  if (reasoningMessage === undefined) {
                    reasoningMessages[reasoningId] = {
                      type: "reasoning",
                      id: reasoningId,
                      encrypted_content: providerOptions?.reasoningEncryptedContent,
                      summary: summaryParts,
                    }
                    input.push(reasoningMessages[reasoningId])
                  } else {
                    reasoningMessage.summary.push(...summaryParts)
                  }
                }
              } else {
                warnings.push({
                  type: "other",
                  message: `Non-OpenAI reasoning parts are not supported. Skipping reasoning part: ${JSON.stringify(part)}.`,
                })
              }
              break
            }
          }
        }

        break
      }

      case "tool": {
        for (const part of content) {
          const output = part.output

          if (hasLocalShellTool && part.toolName === "local_shell" && output.type === "json") {
            input.push({
              type: "local_shell_call_output",
              call_id: part.toolCallId,
              output: localShellOutputSchema.parse(output.value).output,
            })
            break
          }

          let contentValue: string
          switch (output.type) {
            case "text":
            case "error-text":
              contentValue = output.value
              break
            case "content":
            case "json":
            case "error-json":
              contentValue = JSON.stringify(output.value)
              break
          }

          input.push({
            type: "function_call_output",
            call_id: part.toolCallId,
            output: contentValue,
          })
        }

        break
      }

      default: {
        const _exhaustiveCheck: never = role
        throw new Error(`Unsupported role: ${_exhaustiveCheck}`)
      }
    }
  }

  return { input, warnings }
}
```

---

## 6. Response Handling

### 6.1 Chat API Response

**Non-Streaming Response:**

**Response Processing:**
1. Extract text content from `choice.message.content`
2. Extract reasoning from `choice.message.reasoning_text`
3. Extract reasoning state from `choice.message.reasoning_opaque` for multi-turn reasoning
4. Process tool calls from `choice.message.tool_calls`
5. Handle usage tokens including `reasoning_tokens` from `completion_tokens_details`

**Streaming Response:**

Stream events follow Server-Sent Events (SSE) format with chunks containing:
- `delta.content` - Text content deltas
- `delta.reasoning_text` - Reasoning content deltas
- `delta.reasoning_opaque` - Reasoning state (sent once)
- `delta.tool_calls` - Tool call deltas
- `usage` - Token usage (in final chunks or dedicated usage chunks)

Key streaming logic:
- Emit `reasoning-start`, `reasoning-delta`, `reasoning-end` events for reasoning content
- Emit `text-start`, `text-delta`, `text-end` for regular content
- Properly sequence reasoning before text/tool calls
- Attach `reasoning_opaque` to provider metadata on end events

### 6.2 Responses API Response

**Non-Streaming Response:**

The response contains an `output` array with various item types:

**Output Item Types:**
- `{ type: "message", role: "assistant", content: [...] }` - Assistant messages
- `{ type: "reasoning", id, encrypted_content, summary }` - Reasoning content
- `{ type: "function_call", call_id, name, arguments, id }` - Tool calls
- `{ type: "web_search_call", ... }` - Provider-executed web search
- `{ type: "code_interpreter_call", ... }` - Provider-executed code interpreter
- `{ type: "file_search_call", ... }` - Provider-executed file search
- `{ type: "image_generation_call", ... }` - Provider-executed image generation
- `{ type: "local_shell_call", ... }` - Local shell execution
- `{ type: "computer_call", ... }` - Computer use tool

**Response Processing:**
1. Iterate through output items in order
2. Convert reasoning items to reasoning content parts (handle encrypted_content)
3. Convert message items to text content parts (extract annotations for citations)
4. Convert function_call items to tool-call content parts
5. Provider-executed tools (web_search, code_interpreter, etc.) create both tool-call and tool-result parts

**Streaming Response:**

Responses API streaming uses different event types:
- `response.output_item.added` - New item started
- `response.output_item.done` - Item completed
- `response.text.delta` - Text content delta
- `response.reasoning.summary.delta` - Reasoning summary delta
- `response.function_call_arguments.delta` - Tool arguments delta
- `response.done` - Response completed

Important: Track reasoning by `output_index` rather than `item_id` because GitHub Copilot rotates encrypted item IDs on every event.

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L713-741)
```typescript
const OpenAICompatibleChatResponseSchema = z.object({
  id: z.string().nullish(),
  created: z.number().nullish(),
  model: z.string().nullish(),
  choices: z.array(
    z.object({
      message: z.object({
        role: z.literal("assistant").nullish(),
        content: z.string().nullish(),
        // Copilot-specific reasoning fields
        reasoning_text: z.string().nullish(),
        reasoning_opaque: z.string().nullish(),
        tool_calls: z
          .array(
            z.object({
              id: z.string().nullish(),
              function: z.object({
                name: z.string(),
                arguments: z.string(),
              }),
            }),
          )
          .nullish(),
      }),
      finish_reason: z.string().nullish(),
    }),
  ),
  usage: openaiCompatibleTokenUsageSchema,
})
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L216-294)
```typescript
    const choice = responseBody.choices[0]
    const content: Array<LanguageModelV2Content> = []

    // text content:
    const text = choice.message.content
    if (text != null && text.length > 0) {
      content.push({
        type: "text",
        text,
        providerMetadata: choice.message.reasoning_opaque
          ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
          : undefined,
      })
    }

    // reasoning content (Copilot uses reasoning_text):
    const reasoning = choice.message.reasoning_text
    if (reasoning != null && reasoning.length > 0) {
      content.push({
        type: "reasoning",
        text: reasoning,
        // Include reasoning_opaque for Copilot multi-turn reasoning
        providerMetadata: choice.message.reasoning_opaque
          ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
          : undefined,
      })
    }

    // tool calls:
    if (choice.message.tool_calls != null) {
      for (const toolCall of choice.message.tool_calls) {
        content.push({
          type: "tool-call",
          toolCallId: toolCall.id ?? generateId(),
          toolName: toolCall.function.name,
          input: toolCall.function.arguments!,
          providerMetadata: choice.message.reasoning_opaque
            ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
            : undefined,
        })
      }
    }

    // provider metadata:
    const providerMetadata: SharedV2ProviderMetadata = {
      [this.providerOptionsName]: {},
      ...(await this.config.metadataExtractor?.extractMetadata?.({
        parsedBody: rawResponse,
      })),
    }
    const completionTokenDetails = responseBody.usage?.completion_tokens_details
    if (completionTokenDetails?.accepted_prediction_tokens != null) {
      providerMetadata[this.providerOptionsName].acceptedPredictionTokens =
        completionTokenDetails?.accepted_prediction_tokens
    }
    if (completionTokenDetails?.rejected_prediction_tokens != null) {
      providerMetadata[this.providerOptionsName].rejectedPredictionTokens =
        completionTokenDetails?.rejected_prediction_tokens
    }

    return {
      content,
      finishReason: mapOpenAICompatibleFinishReason(choice.finish_reason),
      usage: {
        inputTokens: responseBody.usage?.prompt_tokens ?? undefined,
        outputTokens: responseBody.usage?.completion_tokens ?? undefined,
        totalTokens: responseBody.usage?.total_tokens ?? undefined,
        reasoningTokens: responseBody.usage?.completion_tokens_details?.reasoning_tokens ?? undefined,
        cachedInputTokens: responseBody.usage?.prompt_tokens_details?.cached_tokens ?? undefined,
      },
      providerMetadata,
      request: { body },
      response: {
        ...getResponseMetadata(responseBody),
        headers: responseHeaders,
        body: rawResponse,
      },
      warnings,
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L459-624)
```typescript
            // enqueue reasoning before text deltas (Copilot uses reasoning_text):
            const reasoningContent = delta.reasoning_text
            if (reasoningContent) {
              if (!isActiveReasoning) {
                controller.enqueue({
                  type: "reasoning-start",
                  id: "reasoning-0",
                })
                isActiveReasoning = true
              }

              controller.enqueue({
                type: "reasoning-delta",
                id: "reasoning-0",
                delta: reasoningContent,
              })
            }

            if (delta.content) {
              // If reasoning was active and we're starting text, end reasoning first
              // This handles the case where reasoning_opaque and content come in the same chunk
              if (isActiveReasoning && !isActiveText) {
                controller.enqueue({
                  type: "reasoning-end",
                  id: "reasoning-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveReasoning = false
              }

              if (!isActiveText) {
                controller.enqueue({
                  type: "text-start",
                  id: "txt-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveText = true
              }

              controller.enqueue({
                type: "text-delta",
                id: "txt-0",
                delta: delta.content,
              })
            }

            if (delta.tool_calls != null) {
              // If reasoning was active and we're starting tool calls, end reasoning first
              // This handles the case where reasoning goes directly to tool calls with no content
              if (isActiveReasoning) {
                controller.enqueue({
                  type: "reasoning-end",
                  id: "reasoning-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveReasoning = false
              }
              for (const toolCallDelta of delta.tool_calls) {
                const index = toolCallDelta.index

                if (toolCalls[index] == null) {
                  if (toolCallDelta.id == null) {
                    throw new InvalidResponseDataError({
                      data: toolCallDelta,
                      message: `Expected 'id' to be a string.`,
                    })
                  }

                  if (toolCallDelta.function?.name == null) {
                    throw new InvalidResponseDataError({
                      data: toolCallDelta,
                      message: `Expected 'function.name' to be a string.`,
                    })
                  }

                  controller.enqueue({
                    type: "tool-input-start",
                    id: toolCallDelta.id,
                    toolName: toolCallDelta.function.name,
                  })

                  toolCalls[index] = {
                    id: toolCallDelta.id,
                    type: "function",
                    function: {
                      name: toolCallDelta.function.name,
                      arguments: toolCallDelta.function.arguments ?? "",
                    },
                    hasFinished: false,
                  }

                  const toolCall = toolCalls[index]

                  if (toolCall.function?.name != null && toolCall.function?.arguments != null) {
                    // send delta if the argument text has already started:
                    if (toolCall.function.arguments.length > 0) {
                      controller.enqueue({
                        type: "tool-input-delta",
                        id: toolCall.id,
                        delta: toolCall.function.arguments,
                      })
                    }

                    // check if tool call is complete
                    // (some providers send the full tool call in one chunk):
                    if (isParsableJson(toolCall.function.arguments)) {
                      controller.enqueue({
                        type: "tool-input-end",
                        id: toolCall.id,
                      })

                      controller.enqueue({
                        type: "tool-call",
                        toolCallId: toolCall.id ?? generateId(),
                        toolName: toolCall.function.name,
                        input: toolCall.function.arguments,
                        providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                      })
                      toolCall.hasFinished = true
                    }
                  }

                  continue
                }

                // existing tool call, merge if not finished
                const toolCall = toolCalls[index]

                if (toolCall.hasFinished) {
                  continue
                }

                if (toolCallDelta.function?.arguments != null) {
                  toolCall.function!.arguments += toolCallDelta.function?.arguments ?? ""
                }

                // send delta
                controller.enqueue({
                  type: "tool-input-delta",
                  id: toolCall.id,
                  delta: toolCallDelta.function.arguments ?? "",
                })

                // check if tool call is complete
                if (
                  toolCall.function?.name != null &&
                  toolCall.function?.arguments != null &&
                  isParsableJson(toolCall.function.arguments)
                ) {
                  controller.enqueue({
                    type: "tool-input-end",
                    id: toolCall.id,
                  })

                  controller.enqueue({
                    type: "tool-call",
                    toolCallId: toolCall.id ?? generateId(),
                    toolName: toolCall.function.name,
                    input: toolCall.function.arguments,
                    providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                  })
                  toolCall.hasFinished = true
                }
              }
            }
          },
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L413-493)
```typescript
        z.object({
          id: z.string(),
          created_at: z.number(),
          error: z
            .object({
              code: z.string(),
              message: z.string(),
            })
            .nullish(),
          model: z.string(),
          output: z.array(
            z.discriminatedUnion("type", [
              z.object({
                type: z.literal("message"),
                role: z.literal("assistant"),
                id: z.string(),
                content: z.array(
                  z.object({
                    type: z.literal("output_text"),
                    text: z.string(),
                    logprobs: LOGPROBS_SCHEMA.nullish(),
                    annotations: z.array(
                      z.discriminatedUnion("type", [
                        z.object({
                          type: z.literal("url_citation"),
                          start_index: z.number(),
                          end_index: z.number(),
                          url: z.string(),
                          title: z.string(),
                        }),
                        z.object({
                          type: z.literal("file_citation"),
                          file_id: z.string(),
                          filename: z.string().nullish(),
                          index: z.number().nullish(),
                          start_index: z.number().nullish(),
                          end_index: z.number().nullish(),
                          quote: z.string().nullish(),
                        }),
                        z.object({
                          type: z.literal("container_file_citation"),
                        }),
                      ]),
                    ),
                  }),
                ),
              }),
              webSearchCallItem,
              fileSearchCallItem,
              codeInterpreterCallItem,
              imageGenerationCallItem,
              localShellCallItem,
              z.object({
                type: z.literal("function_call"),
                call_id: z.string(),
                name: z.string(),
                arguments: z.string(),
                id: z.string(),
              }),
              z.object({
                type: z.literal("computer_call"),
                id: z.string(),
                status: z.string().optional(),
              }),
              z.object({
                type: z.literal("reasoning"),
                id: z.string(),
                encrypted_content: z.string().nullish(),
                summary: z.array(
                  z.object({
                    type: z.literal("summary_text"),
                    text: z.string(),
                  }),
                ),
              }),
            ]),
          ),
          service_tier: z.string().nullish(),
          incomplete_details: z.object({ reason: z.string() }).nullish(),
          usage: usageSchema,
        }),
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L518-732)
```typescript
    for (const part of response.output) {
      switch (part.type) {
        case "reasoning": {
          // when there are no summary parts, we need to add an empty reasoning part:
          if (part.summary.length === 0) {
            part.summary.push({ type: "summary_text", text: "" })
          }

          for (const summary of part.summary) {
            content.push({
              type: "reasoning" as const,
              text: summary.text,
              providerMetadata: {
                openai: {
                  itemId: part.id,
                  reasoningEncryptedContent: part.encrypted_content ?? null,
                },
              },
            })
          }
          break
        }

        case "image_generation_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "image_generation",
            input: "{}",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "image_generation",
            result: {
              result: part.result,
            } satisfies z.infer<typeof imageGenerationOutputSchema>,
            providerExecuted: true,
          })

          break
        }

        case "local_shell_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.call_id,
            toolName: "local_shell",
            input: JSON.stringify({ action: part.action } satisfies z.infer<typeof localShellInputSchema>),
            providerMetadata: {
              openai: {
                itemId: part.id,
              },
            },
          })

          break
        }

        case "message": {
          for (const contentPart of part.content) {
            if (options.providerOptions?.openai?.logprobs && contentPart.logprobs) {
              logprobs.push(contentPart.logprobs)
            }

            content.push({
              type: "text",
              text: contentPart.text,
              providerMetadata: {
                openai: {
                  itemId: part.id,
                },
              },
            })

            for (const annotation of contentPart.annotations) {
              if (annotation.type === "url_citation") {
                content.push({
                  type: "source",
                  sourceType: "url",
                  id: this.config.generateId?.() ?? generateId(),
                  url: annotation.url,
                  title: annotation.title,
                })
              } else if (annotation.type === "file_citation") {
                content.push({
                  type: "source",
                  sourceType: "document",
                  id: this.config.generateId?.() ?? generateId(),
                  mediaType: "text/plain",
                  title: annotation.quote ?? annotation.filename ?? "Document",
                  filename: annotation.filename ?? annotation.file_id,
                })
              }
            }
          }

          break
        }

        case "function_call": {
          hasFunctionCall = true

          content.push({
            type: "tool-call",
            toolCallId: part.call_id,
            toolName: part.name,
            input: part.arguments,
            providerMetadata: {
              openai: {
                itemId: part.id,
              },
            },
          })
          break
        }

        case "web_search_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: webSearchToolName ?? "web_search",
            input: JSON.stringify({ action: part.action }),
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: webSearchToolName ?? "web_search",
            result: { status: part.status },
            providerExecuted: true,
          })

          break
        }

        case "computer_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "computer_use",
            input: "",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "computer_use",
            result: {
              type: "computer_use_tool_result",
              status: part.status || "completed",
            },
            providerExecuted: true,
          })
          break
        }

        case "file_search_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "file_search",
            input: "{}",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "file_search",
            result: {
              queries: part.queries,
              results:
                part.results?.map((result) => ({
                  attributes: result.attributes,
                  fileId: result.file_id,
                  filename: result.filename,
                  score: result.score,
                  text: result.text,
                })) ?? null,
            } satisfies z.infer<typeof fileSearchOutputSchema>,
            providerExecuted: true,
          })
          break
        }

        case "code_interpreter_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "code_interpreter",
            input: JSON.stringify({
              code: part.code,
              containerId: part.container_id,
            } satisfies z.infer<typeof codeInterpreterInputSchema>),
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "code_interpreter",
            result: {
              outputs: part.outputs,
            } satisfies z.infer<typeof codeInterpreterOutputSchema>,
            providerExecuted: true,
          })
          break
        }
      }
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L818-831)
```typescript
    // Track reasoning by output_index instead of item_id
    // GitHub Copilot rotates encrypted item IDs on every event
    const activeReasoning: Record<
      number,
      {
        canonicalId: string // the item.id from output_item.added
        encryptedContent?: string | null
        summaryParts: number[]
      }
    > = {}

    // Track current active reasoning output_index for correlating summary events
    let currentReasoningOutputIndex: number | null = null
```

---

## 7. Premium Features & Subscription Handling

### Model Access Requirements

Some Copilot models require specific subscription tiers:
- **Pro+ subscription** may be required for advanced models
- Models must be manually enabled in GitHub Copilot settings at: `https://github.com/settings/copilot/features`

There is no programmatic way to check subscription status - the API will return 403 errors for unauthorized model access.

### Error Handling for Subscription Issues

**403 Forbidden:**
Indicates authentication or subscription issues. Suggest users re-authenticate or verify their Copilot subscription.

**Model Not Supported:**
When a model isn't available (not enabled in settings), suggest users enable it in their Copilot settings.

### Reasoning Effort Variants

Models support different reasoning effort levels as variants:

**Standard models support:** `low`, `medium`, `high`

**Advanced models (gpt-5.2, gpt-5.3, gpt-5.1-codex-max) support:** `low`, `medium`, `high`, `xhigh`

**Claude models support:** Custom `thinking_budget` parameter (e.g., 4000 tokens)

These are configured as provider options:
```typescript
{
  reasoningEffort: "medium",
  reasoningSummary: "auto",
  include: ["reasoning.encrypted_content"]
}
```

### Service Tiers

The Responses API supports service tiers for priority processing:

**Flex Processing:** Available for o3, o4-mini, and gpt-5 models
**Priority Processing:** Available for gpt-4, gpt-5, gpt-5-mini, o3, o4-mini with Enterprise access (not available for gpt-5-nano)

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/transform.ts (L367-391)
```typescript
      case "@ai-sdk/github-copilot":
        if (model.id.includes("gemini")) {
          // currently github copilot only returns thinking
          return {}
        }
        if (model.id.includes("claude")) {
          return {
            thinking: { thinking_budget: 4000 },
          }
        }
        const copilotEfforts = iife(() => {
          if (id.includes("5.1-codex-max") || id.includes("5.2") || id.includes("5.3"))
            return [...WIDELY_SUPPORTED_EFFORTS, "xhigh"]
          return WIDELY_SUPPORTED_EFFORTS
        })
        return Object.fromEntries(
          copilotEfforts.map((effort) => [
            effort,
            {
              reasoningEffort: effort,
              reasoningSummary: "auto",
              include: ["reasoning.encrypted_content"],
            },
          ]),
        )
```

**File:** packages/opencode/src/provider/transform.ts (L816-824)
```typescript
    if (providerID.includes("github-copilot") && error.statusCode === 403) {
      return "Please reauthenticate with the copilot provider to ensure your credentials work properly with OpenCode."
    }
    if (providerID.includes("github-copilot") && message.includes("The requested model is not supported")) {
      return (
        message +
        "\n\nMake sure the model is enabled in your copilot settings: https://github.com/settings/copilot/features"
      )
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L350-371)
```typescript
    // Validate flex processing support
    if (openaiOptions?.serviceTier === "flex" && !modelConfig.supportsFlexProcessing) {
      warnings.push({
        type: "unsupported-setting",
        setting: "serviceTier",
        details: "flex processing is only available for o3, o4-mini, and gpt-5 models",
      })
      // Remove from args if not supported
      delete (baseArgs as any).service_tier
    }

    // Validate priority processing support
    if (openaiOptions?.serviceTier === "priority" && !modelConfig.supportsPriorityProcessing) {
      warnings.push({
        type: "unsupported-setting",
        setting: "serviceTier",
        details:
          "priority processing is only available for supported models (gpt-4, gpt-5, gpt-5-mini, o3, o4-mini) and requires Enterprise access. gpt-5-nano is not supported",
      })
      // Remove from args if not supported
      delete (baseArgs as any).service_tier
    }
```

---

## 8. Enterprise Support

### Deployment Type Selection

During authentication, prompt users to select deployment type:
1. **GitHub.com** - Public GitHub
2. **GitHub Enterprise** - Self-hosted or data residency

### Enterprise URL Configuration

For Enterprise deployments:
1. Prompt for enterprise URL or domain (e.g., `company.ghe.com` or `https://company.ghe.com`)
2. Normalize the domain by removing protocol and trailing slashes
3. Use normalized domain for OAuth endpoints
4. Construct Copilot API base URL as `https://copilot-api.{normalized-domain}`

### Dual Provider Support

Store enterprise authentication separately:
- Provider ID: `github-copilot-enterprise`
- Store `enterpriseUrl` with the auth credentials
- Return provider in auth success response

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/plugin/copilot.ts (L148-163)
```typescript
            {
              type: "select",
              key: "deploymentType",
              message: "Select GitHub deployment type",
              options: [
                {
                  label: "GitHub.com",
                  value: "github.com",
                  hint: "Public",
                },
                {
                  label: "GitHub Enterprise",
                  value: "enterprise",
                  hint: "Data residency or self-hosted",
                },
              ],
```

**File:** packages/opencode/src/plugin/copilot.ts (L165-181)
```typescript
            {
              type: "text",
              key: "enterpriseUrl",
              message: "Enter your GitHub Enterprise URL or domain",
              placeholder: "company.ghe.com or https://company.ghe.com",
              condition: (inputs) => inputs.deploymentType === "enterprise",
              validate: (value) => {
                if (!value) return "URL or domain is required"
                try {
                  const url = value.includes("://") ? new URL(value) : new URL(`https://${value}`)
                  if (!url.hostname) return "Please enter a valid URL or domain"
                  return undefined
                } catch {
                  return "Please enter a valid URL (e.g., company.ghe.com or https://company.ghe.com)"
                }
              },
            },
```

**File:** packages/opencode/src/plugin/copilot.ts (L184-193)
```typescript
            const deploymentType = inputs.deploymentType || "github.com"

            let domain = "github.com"
            let actualProvider = "github-copilot"

            if (deploymentType === "enterprise") {
              const enterpriseUrl = inputs.enterpriseUrl
              domain = normalizeDomain(enterpriseUrl!)
              actualProvider = "github-copilot-enterprise"
            }
```

---

## 9. Additional Implementation Notes

### SDK Integration

The implementation uses a custom OpenAI-compatible SDK wrapper that:
1. Supports both `.chat()` and `.responses()` methods
2. Handles Copilot-specific fields (reasoning_text, reasoning_opaque)
3. Provides custom error handling

### Provider Options Mapping

When using the Copilot provider, provider-specific options should be namespaced under `copilot` in provider options.

### Subagent Sessions

When making requests for subagent/nested sessions (non-user-initiated), mark them as agent-initiated to match Copilot tool conventions.

### Default Model Selection

For small model selection (fast operations), prioritize free models on Copilot.

### Reference Excerpts (OpenCode)

**File:** packages/opencode/src/provider/sdk/copilot/copilot-provider.ts (L49-97)
```typescript
/**
 * Create an OpenAI Compatible provider instance.
 */
export function createOpenaiCompatible(options: OpenaiCompatibleProviderSettings = {}): OpenaiCompatibleProvider {
  const baseURL = withoutTrailingSlash(options.baseURL ?? "https://api.openai.com/v1")

  if (!baseURL) {
    throw new Error("baseURL is required")
  }

  // Merge headers: defaults first, then user overrides
  const headers = {
    // Default OpenAI Compatible headers (can be overridden by user)
    ...(options.apiKey && { Authorization: `Bearer ${options.apiKey}` }),
    ...options.headers,
  }

  const getHeaders = () => withUserAgentSuffix(headers, `ai-sdk/openai-compatible/${VERSION}`)

  const createChatModel = (modelId: OpenaiCompatibleModelId) => {
    return new OpenAICompatibleChatLanguageModel(modelId, {
      provider: `${options.name ?? "openai-compatible"}.chat`,
      headers: getHeaders,
      url: ({ path }) => `${baseURL}${path}`,
      fetch: options.fetch,
    })
  }

  const createResponsesModel = (modelId: OpenaiCompatibleModelId) => {
    return new OpenAIResponsesLanguageModel(modelId, {
      provider: `${options.name ?? "openai-compatible"}.responses`,
      headers: getHeaders,
      url: ({ path }) => `${baseURL}${path}`,
      fetch: options.fetch,
    })
  }

  const createLanguageModel = (modelId: OpenaiCompatibleModelId) => createChatModel(modelId)

  const provider = function (modelId: OpenaiCompatibleModelId) {
    return createChatModel(modelId)
  }

  provider.languageModel = createLanguageModel
  provider.chat = createChatModel
  provider.responses = createResponsesModel

  return provider as OpenaiCompatibleProvider
}
```

**File:** packages/opencode/src/provider/transform.ts (L21-25)
```typescript
  function sdkKey(npm: string): string | undefined {
    switch (npm) {
      case "@ai-sdk/github-copilot":
        return "copilot"
      case "@ai-sdk/openai":
```

**File:** packages/opencode/src/plugin/copilot.ts (L311-325)
```typescript
      const session = await sdk.session
        .get({
          path: {
            id: incoming.sessionID,
          },
          query: {
            directory: input.directory,
          },
          throwOnError: true,
        })
        .catch(() => undefined)
      if (!session || !session.data.parentID) return
      // mark subagent sessions as agent initiated matching standard that other copilot tools have
      output.headers["x-initiator"] = "agent"
    },
```

**File:** packages/opencode/src/provider/provider.ts (L1169-1172)
```typescript
      if (providerID.startsWith("github-copilot")) {
        // prioritize free models for github copilot
        priority = ["gpt-5-mini", "claude-haiku-4.5", ...priority]
      }
```

---

## Notes

This specification covers all major aspects of GitHub Copilot integration. Key implementation considerations:

1. **OAuth Flow**: Implement proper device flow with polling intervals and error handling
2. **API Selection**: Always check model ID to route between Chat and Responses APIs
3. **Header Injection**: Custom fetch wrapper is essential for auth and metadata headers
4. **Reasoning State**: Preserve `reasoning_opaque` across turns for multi-turn reasoning
5. **Enterprise Support**: Handle both standard and enterprise deployments with separate auth
6. **Error Handling**: Provide clear guidance on subscription and model availability issues
7. **Streaming**: Handle different streaming formats between Chat and Responses APIs
8. **Provider-Executed Tools**: Responses API includes built-in tools like web search and code interpreter

The implementation should be compatible with AI SDK v2 (Vercel AI SDK) architecture for maximum ecosystem compatibility.

### Appendix: Full Reference Excerpts (OpenCode)

The excerpts below are retained for completeness; key excerpts are embedded above in the relevant sections for easier reading.

**File:** packages/opencode/src/plugin/copilot.ts (L5-7)
```typescript
const CLIENT_ID = "Ov23li8tweQw6odWQebz"
// Add a small safety buffer when polling to avoid hitting the server
// slightly too early due to clock skew / timer drift.
```

**File:** packages/opencode/src/plugin/copilot.ts (L29-30)
```typescript
        const enterpriseUrl = info.enterpriseUrl
        const baseURL = enterpriseUrl ? `https://copilot-api.${normalizeDomain(enterpriseUrl)}` : undefined
```

**File:** packages/opencode/src/plugin/copilot.ts (L68-119)
```typescript
            const { isVision, isAgent } = iife(() => {
              try {
                const body = typeof init?.body === "string" ? JSON.parse(init.body) : init?.body

                // Completions API
                if (body?.messages && url.includes("completions")) {
                  const last = body.messages[body.messages.length - 1]
                  return {
                    isVision: body.messages.some(
                      (msg: any) =>
                        Array.isArray(msg.content) && msg.content.some((part: any) => part.type === "image_url"),
                    ),
                    isAgent: last?.role !== "user",
                  }
                }

                // Responses API
                if (body?.input) {
                  const last = body.input[body.input.length - 1]
                  return {
                    isVision: body.input.some(
                      (item: any) =>
                        Array.isArray(item?.content) && item.content.some((part: any) => part.type === "input_image"),
                    ),
                    isAgent: last?.role !== "user",
                  }
                }

                // Messages API
                if (body?.messages) {
                  const last = body.messages[body.messages.length - 1]
                  const hasNonToolCalls =
                    Array.isArray(last?.content) && last.content.some((part: any) => part?.type !== "tool_result")
                  return {
                    isVision: body.messages.some(
                      (item: any) =>
                        Array.isArray(item?.content) &&
                        item.content.some(
                          (part: any) =>
                            part?.type === "image" ||
                            // images can be nested inside tool_result content
                            (part?.type === "tool_result" &&
                              Array.isArray(part?.content) &&
                              part.content.some((nested: any) => nested?.type === "image")),
                        ),
                    ),
                    isAgent: !(last?.role === "user" && hasNonToolCalls),
                  }
                }
              } catch {}
              return { isVision: false, isAgent: false }
            })
```

**File:** packages/opencode/src/plugin/copilot.ts (L121-139)
```typescript
            const headers: Record<string, string> = {
              "x-initiator": isAgent ? "agent" : "user",
              ...(init?.headers as Record<string, string>),
              "User-Agent": `opencode/${Installation.VERSION}`,
              Authorization: `Bearer ${info.refresh}`,
              "Openai-Intent": "conversation-edits",
            }

            if (isVision) {
              headers["Copilot-Vision-Request"] = "true"
            }

            delete headers["x-api-key"]
            delete headers["authorization"]

            return fetch(request, {
              ...init,
              headers,
            })
```

**File:** packages/opencode/src/plugin/copilot.ts (L148-163)
```typescript
            {
              type: "select",
              key: "deploymentType",
              message: "Select GitHub deployment type",
              options: [
                {
                  label: "GitHub.com",
                  value: "github.com",
                  hint: "Public",
                },
                {
                  label: "GitHub Enterprise",
                  value: "enterprise",
                  hint: "Data residency or self-hosted",
                },
              ],
```

**File:** packages/opencode/src/plugin/copilot.ts (L165-181)
```typescript
            {
              type: "text",
              key: "enterpriseUrl",
              message: "Enter your GitHub Enterprise URL or domain",
              placeholder: "company.ghe.com or https://company.ghe.com",
              condition: (inputs) => inputs.deploymentType === "enterprise",
              validate: (value) => {
                if (!value) return "URL or domain is required"
                try {
                  const url = value.includes("://") ? new URL(value) : new URL(`https://${value}`)
                  if (!url.hostname) return "Please enter a valid URL or domain"
                  return undefined
                } catch {
                  return "Please enter a valid URL (e.g., company.ghe.com or https://company.ghe.com)"
                }
              },
            },
```

**File:** packages/opencode/src/plugin/copilot.ts (L184-193)
```typescript
            const deploymentType = inputs.deploymentType || "github.com"

            let domain = "github.com"
            let actualProvider = "github-copilot"

            if (deploymentType === "enterprise") {
              const enterpriseUrl = inputs.enterpriseUrl
              domain = normalizeDomain(enterpriseUrl!)
              actualProvider = "github-copilot-enterprise"
            }
```

**File:** packages/opencode/src/plugin/copilot.ts (L197-208)
```typescript
            const deviceResponse = await fetch(urls.DEVICE_CODE_URL, {
              method: "POST",
              headers: {
                Accept: "application/json",
                "Content-Type": "application/json",
                "User-Agent": `opencode/${Installation.VERSION}`,
              },
              body: JSON.stringify({
                client_id: CLIENT_ID,
                scope: "read:user",
              }),
            })
```

**File:** packages/opencode/src/plugin/copilot.ts (L214-223)
```typescript
            const deviceData = (await deviceResponse.json()) as {
              verification_uri: string
              user_code: string
              device_code: string
              interval: number
            }

            return {
              url: deviceData.verification_uri,
              instructions: `Enter code: ${deviceData.user_code}`,
```

**File:** packages/opencode/src/plugin/copilot.ts (L226-297)
```typescript
                while (true) {
                  const response = await fetch(urls.ACCESS_TOKEN_URL, {
                    method: "POST",
                    headers: {
                      Accept: "application/json",
                      "Content-Type": "application/json",
                      "User-Agent": `opencode/${Installation.VERSION}`,
                    },
                    body: JSON.stringify({
                      client_id: CLIENT_ID,
                      device_code: deviceData.device_code,
                      grant_type: "urn:ietf:params:oauth:grant-type:device_code",
                    }),
                  })

                  if (!response.ok) return { type: "failed" as const }

                  const data = (await response.json()) as {
                    access_token?: string
                    error?: string
                    interval?: number
                  }

                  if (data.access_token) {
                    const result: {
                      type: "success"
                      refresh: string
                      access: string
                      expires: number
                      provider?: string
                      enterpriseUrl?: string
                    } = {
                      type: "success",
                      refresh: data.access_token,
                      access: data.access_token,
                      expires: 0,
                    }

                    if (actualProvider === "github-copilot-enterprise") {
                      result.provider = "github-copilot-enterprise"
                      result.enterpriseUrl = domain
                    }

                    return result
                  }

                  if (data.error === "authorization_pending") {
                    await Bun.sleep(deviceData.interval * 1000 + OAUTH_POLLING_SAFETY_MARGIN_MS)
                    continue
                  }

                  if (data.error === "slow_down") {
                    // Based on the RFC spec, we must add 5 seconds to our current polling interval.
                    // (See https://www.rfc-editor.org/rfc/rfc8628#section-3.5)
                    let newInterval = (deviceData.interval + 5) * 1000

                    // GitHub OAuth API may return the new interval in seconds in the response.
                    // We should try to use that if provided with safety margin.
                    const serverInterval = data.interval
                    if (serverInterval && typeof serverInterval === "number" && serverInterval > 0) {
                      newInterval = serverInterval * 1000
                    }

                    await Bun.sleep(newInterval + OAUTH_POLLING_SAFETY_MARGIN_MS)
                    continue
                  }

                  if (data.error) return { type: "failed" as const }

                  await Bun.sleep(deviceData.interval * 1000 + OAUTH_POLLING_SAFETY_MARGIN_MS)
                  continue
                }
```

**File:** packages/opencode/src/plugin/copilot.ts (L311-325)
```typescript
      const session = await sdk.session
        .get({
          path: {
            id: incoming.sessionID,
          },
          query: {
            directory: input.directory,
          },
          throwOnError: true,
        })
        .catch(() => undefined)
      if (!session || !session.data.parentID) return
      // mark subagent sessions as agent initiated matching standard that other copilot tools have
      output.headers["x-initiator"] = "agent"
    },
```

**File:** packages/opencode/src/provider/provider.ts (L46-56)
```typescript
  function isGpt5OrLater(modelID: string): boolean {
    const match = /^gpt-(\d+)/.exec(modelID)
    if (!match) {
      return false
    }
    return Number(match[1]) >= 5
  }

  function shouldUseCopilotResponsesApi(modelID: string): boolean {
    return isGpt5OrLater(modelID) && !modelID.startsWith("gpt-5-mini")
  }
```

**File:** packages/opencode/src/provider/provider.ts (L133-142)
```typescript
    "github-copilot": async () => {
      return {
        autoload: false,
        async getModel(sdk: any, modelID: string, _options?: Record<string, any>) {
          if (sdk.responses === undefined && sdk.chat === undefined) return sdk.languageModel(modelID)
          return shouldUseCopilotResponsesApi(modelID) ? sdk.responses(modelID) : sdk.chat(modelID)
        },
        options: {},
      }
    },
```

**File:** packages/opencode/src/provider/provider.ts (L723-735)
```typescript
    // Add GitHub Copilot Enterprise provider that inherits from GitHub Copilot
    if (database["github-copilot"]) {
      const githubCopilot = database["github-copilot"]
      database["github-copilot-enterprise"] = {
        ...githubCopilot,
        id: "github-copilot-enterprise",
        name: "GitHub Copilot Enterprise",
        models: mapValues(githubCopilot.models, (model) => ({
          ...model,
          providerID: "github-copilot-enterprise",
        })),
      }
    }
```

**File:** packages/opencode/src/provider/provider.ts (L1169-1172)
```typescript
      if (providerID.startsWith("github-copilot")) {
        // prioritize free models for github copilot
        priority = ["gpt-5-mini", "claude-haiku-4.5", ...priority]
      }
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L140-189)
```typescript
      args: {
        // model id:
        model: this.modelId,

        // model specific settings:
        user: compatibleOptions.user,

        // standardized settings:
        max_tokens: maxOutputTokens,
        temperature,
        top_p: topP,
        frequency_penalty: frequencyPenalty,
        presence_penalty: presencePenalty,
        response_format:
          responseFormat?.type === "json"
            ? this.supportsStructuredOutputs === true && responseFormat.schema != null
              ? {
                  type: "json_schema",
                  json_schema: {
                    schema: responseFormat.schema,
                    name: responseFormat.name ?? "response",
                    description: responseFormat.description,
                  },
                }
              : { type: "json_object" }
            : undefined,

        stop: stopSequences,
        seed,
        ...Object.fromEntries(
          Object.entries(providerOptions?.[this.providerOptionsName] ?? {}).filter(
            ([key]) => !Object.keys(openaiCompatibleProviderOptions.shape).includes(key),
          ),
        ),

        reasoning_effort: compatibleOptions.reasoningEffort,
        verbosity: compatibleOptions.textVerbosity,

        // messages:
        messages: convertToOpenAICompatibleChatMessages(prompt),

        // tools:
        tools: openaiTools,
        tool_choice: openaiToolChoice,

        // thinking_budget
        thinking_budget: compatibleOptions.thinking_budget,
      },
      warnings: [...warnings, ...toolWarnings],
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L216-294)
```typescript
    const choice = responseBody.choices[0]
    const content: Array<LanguageModelV2Content> = []

    // text content:
    const text = choice.message.content
    if (text != null && text.length > 0) {
      content.push({
        type: "text",
        text,
        providerMetadata: choice.message.reasoning_opaque
          ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
          : undefined,
      })
    }

    // reasoning content (Copilot uses reasoning_text):
    const reasoning = choice.message.reasoning_text
    if (reasoning != null && reasoning.length > 0) {
      content.push({
        type: "reasoning",
        text: reasoning,
        // Include reasoning_opaque for Copilot multi-turn reasoning
        providerMetadata: choice.message.reasoning_opaque
          ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
          : undefined,
      })
    }

    // tool calls:
    if (choice.message.tool_calls != null) {
      for (const toolCall of choice.message.tool_calls) {
        content.push({
          type: "tool-call",
          toolCallId: toolCall.id ?? generateId(),
          toolName: toolCall.function.name,
          input: toolCall.function.arguments!,
          providerMetadata: choice.message.reasoning_opaque
            ? { copilot: { reasoningOpaque: choice.message.reasoning_opaque } }
            : undefined,
        })
      }
    }

    // provider metadata:
    const providerMetadata: SharedV2ProviderMetadata = {
      [this.providerOptionsName]: {},
      ...(await this.config.metadataExtractor?.extractMetadata?.({
        parsedBody: rawResponse,
      })),
    }
    const completionTokenDetails = responseBody.usage?.completion_tokens_details
    if (completionTokenDetails?.accepted_prediction_tokens != null) {
      providerMetadata[this.providerOptionsName].acceptedPredictionTokens =
        completionTokenDetails?.accepted_prediction_tokens
    }
    if (completionTokenDetails?.rejected_prediction_tokens != null) {
      providerMetadata[this.providerOptionsName].rejectedPredictionTokens =
        completionTokenDetails?.rejected_prediction_tokens
    }

    return {
      content,
      finishReason: mapOpenAICompatibleFinishReason(choice.finish_reason),
      usage: {
        inputTokens: responseBody.usage?.prompt_tokens ?? undefined,
        outputTokens: responseBody.usage?.completion_tokens ?? undefined,
        totalTokens: responseBody.usage?.total_tokens ?? undefined,
        reasoningTokens: responseBody.usage?.completion_tokens_details?.reasoning_tokens ?? undefined,
        cachedInputTokens: responseBody.usage?.prompt_tokens_details?.cached_tokens ?? undefined,
      },
      providerMetadata,
      request: { body },
      response: {
        ...getResponseMetadata(responseBody),
        headers: responseHeaders,
        body: rawResponse,
      },
      warnings,
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L459-624)
```typescript
            // enqueue reasoning before text deltas (Copilot uses reasoning_text):
            const reasoningContent = delta.reasoning_text
            if (reasoningContent) {
              if (!isActiveReasoning) {
                controller.enqueue({
                  type: "reasoning-start",
                  id: "reasoning-0",
                })
                isActiveReasoning = true
              }

              controller.enqueue({
                type: "reasoning-delta",
                id: "reasoning-0",
                delta: reasoningContent,
              })
            }

            if (delta.content) {
              // If reasoning was active and we're starting text, end reasoning first
              // This handles the case where reasoning_opaque and content come in the same chunk
              if (isActiveReasoning && !isActiveText) {
                controller.enqueue({
                  type: "reasoning-end",
                  id: "reasoning-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveReasoning = false
              }

              if (!isActiveText) {
                controller.enqueue({
                  type: "text-start",
                  id: "txt-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveText = true
              }

              controller.enqueue({
                type: "text-delta",
                id: "txt-0",
                delta: delta.content,
              })
            }

            if (delta.tool_calls != null) {
              // If reasoning was active and we're starting tool calls, end reasoning first
              // This handles the case where reasoning goes directly to tool calls with no content
              if (isActiveReasoning) {
                controller.enqueue({
                  type: "reasoning-end",
                  id: "reasoning-0",
                  providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                })
                isActiveReasoning = false
              }
              for (const toolCallDelta of delta.tool_calls) {
                const index = toolCallDelta.index

                if (toolCalls[index] == null) {
                  if (toolCallDelta.id == null) {
                    throw new InvalidResponseDataError({
                      data: toolCallDelta,
                      message: `Expected 'id' to be a string.`,
                    })
                  }

                  if (toolCallDelta.function?.name == null) {
                    throw new InvalidResponseDataError({
                      data: toolCallDelta,
                      message: `Expected 'function.name' to be a string.`,
                    })
                  }

                  controller.enqueue({
                    type: "tool-input-start",
                    id: toolCallDelta.id,
                    toolName: toolCallDelta.function.name,
                  })

                  toolCalls[index] = {
                    id: toolCallDelta.id,
                    type: "function",
                    function: {
                      name: toolCallDelta.function.name,
                      arguments: toolCallDelta.function.arguments ?? "",
                    },
                    hasFinished: false,
                  }

                  const toolCall = toolCalls[index]

                  if (toolCall.function?.name != null && toolCall.function?.arguments != null) {
                    // send delta if the argument text has already started:
                    if (toolCall.function.arguments.length > 0) {
                      controller.enqueue({
                        type: "tool-input-delta",
                        id: toolCall.id,
                        delta: toolCall.function.arguments,
                      })
                    }

                    // check if tool call is complete
                    // (some providers send the full tool call in one chunk):
                    if (isParsableJson(toolCall.function.arguments)) {
                      controller.enqueue({
                        type: "tool-input-end",
                        id: toolCall.id,
                      })

                      controller.enqueue({
                        type: "tool-call",
                        toolCallId: toolCall.id ?? generateId(),
                        toolName: toolCall.function.name,
                        input: toolCall.function.arguments,
                        providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                      })
                      toolCall.hasFinished = true
                    }
                  }

                  continue
                }

                // existing tool call, merge if not finished
                const toolCall = toolCalls[index]

                if (toolCall.hasFinished) {
                  continue
                }

                if (toolCallDelta.function?.arguments != null) {
                  toolCall.function!.arguments += toolCallDelta.function?.arguments ?? ""
                }

                // send delta
                controller.enqueue({
                  type: "tool-input-delta",
                  id: toolCall.id,
                  delta: toolCallDelta.function.arguments ?? "",
                })

                // check if tool call is complete
                if (
                  toolCall.function?.name != null &&
                  toolCall.function?.arguments != null &&
                  isParsableJson(toolCall.function.arguments)
                ) {
                  controller.enqueue({
                    type: "tool-input-end",
                    id: toolCall.id,
                  })

                  controller.enqueue({
                    type: "tool-call",
                    toolCallId: toolCall.id ?? generateId(),
                    toolName: toolCall.function.name,
                    input: toolCall.function.arguments,
                    providerMetadata: reasoningOpaque ? { copilot: { reasoningOpaque } } : undefined,
                  })
                  toolCall.hasFinished = true
                }
              }
            }
          },
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-chat-language-model.ts (L713-741)
```typescript
const OpenAICompatibleChatResponseSchema = z.object({
  id: z.string().nullish(),
  created: z.number().nullish(),
  model: z.string().nullish(),
  choices: z.array(
    z.object({
      message: z.object({
        role: z.literal("assistant").nullish(),
        content: z.string().nullish(),
        // Copilot-specific reasoning fields
        reasoning_text: z.string().nullish(),
        reasoning_opaque: z.string().nullish(),
        tool_calls: z
          .array(
            z.object({
              id: z.string().nullish(),
              function: z.object({
                name: z.string(),
                arguments: z.string(),
              }),
            }),
          )
          .nullish(),
      }),
      finish_reason: z.string().nullish(),
    }),
  ),
  usage: openaiCompatibleTokenUsageSchema,
})
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/openai-compatible-api-types.ts (L1-64)
```typescript
import type { JSONValue } from "@ai-sdk/provider"

export type OpenAICompatibleChatPrompt = Array<OpenAICompatibleMessage>

export type OpenAICompatibleMessage =
  | OpenAICompatibleSystemMessage
  | OpenAICompatibleUserMessage
  | OpenAICompatibleAssistantMessage
  | OpenAICompatibleToolMessage

// Allow for arbitrary additional properties for general purpose
// provider-metadata-specific extensibility.
type JsonRecord<T = never> = Record<string, JSONValue | JSONValue[] | T | T[] | undefined>

export interface OpenAICompatibleSystemMessage extends JsonRecord<OpenAICompatibleSystemContentPart> {
  role: "system"
  content: string | Array<OpenAICompatibleSystemContentPart>
}

export interface OpenAICompatibleSystemContentPart extends JsonRecord {
  type: "text"
  text: string
}

export interface OpenAICompatibleUserMessage extends JsonRecord<OpenAICompatibleContentPart> {
  role: "user"
  content: string | Array<OpenAICompatibleContentPart>
}

export type OpenAICompatibleContentPart = OpenAICompatibleContentPartText | OpenAICompatibleContentPartImage

export interface OpenAICompatibleContentPartImage extends JsonRecord {
  type: "image_url"
  image_url: { url: string }
}

export interface OpenAICompatibleContentPartText extends JsonRecord {
  type: "text"
  text: string
}

export interface OpenAICompatibleAssistantMessage extends JsonRecord<OpenAICompatibleMessageToolCall> {
  role: "assistant"
  content?: string | null
  tool_calls?: Array<OpenAICompatibleMessageToolCall>
  // Copilot-specific reasoning fields
  reasoning_text?: string
  reasoning_opaque?: string
}

export interface OpenAICompatibleMessageToolCall extends JsonRecord {
  type: "function"
  id: string
  function: {
    arguments: string
    name: string
  }
}

export interface OpenAICompatibleToolMessage extends JsonRecord {
  role: "tool"
  content: string
  tool_call_id: string
}
```

**File:** packages/opencode/src/provider/sdk/copilot/chat/convert-to-openai-compatible-chat-messages.ts (L73-124)
```typescript
      case "assistant": {
        let text = ""
        let reasoningText: string | undefined
        let reasoningOpaque: string | undefined
        const toolCalls: Array<{
          id: string
          type: "function"
          function: { name: string; arguments: string }
        }> = []

        for (const part of content) {
          const partMetadata = getOpenAIMetadata(part)
          // Check for reasoningOpaque on any part (may be attached to text/tool-call)
          const partOpaque = (part.providerOptions as { copilot?: { reasoningOpaque?: string } })?.copilot
            ?.reasoningOpaque
          if (partOpaque && !reasoningOpaque) {
            reasoningOpaque = partOpaque
          }

          switch (part.type) {
            case "text": {
              text += part.text
              break
            }
            case "reasoning": {
              if (part.text) reasoningText = part.text
              break
            }
            case "tool-call": {
              toolCalls.push({
                id: part.toolCallId,
                type: "function",
                function: {
                  name: part.toolName,
                  arguments: JSON.stringify(part.input),
                },
                ...partMetadata,
              })
              break
            }
          }
        }

        messages.push({
          role: "assistant",
          content: text || null,
          tool_calls: toolCalls.length > 0 ? toolCalls : undefined,
          reasoning_text: reasoningOpaque ? reasoningText : undefined,
          reasoning_opaque: reasoningOpaque,
          ...metadata,
        })

```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L254-310)
```typescript
    const baseArgs = {
      model: this.modelId,
      input,
      temperature,
      top_p: topP,
      max_output_tokens: maxOutputTokens,

      ...((responseFormat?.type === "json" || openaiOptions?.textVerbosity) && {
        text: {
          ...(responseFormat?.type === "json" && {
            format:
              responseFormat.schema != null
                ? {
                    type: "json_schema",
                    strict: strictJsonSchema,
                    name: responseFormat.name ?? "response",
                    description: responseFormat.description,
                    schema: responseFormat.schema,
                  }
                : { type: "json_object" },
          }),
          ...(openaiOptions?.textVerbosity && {
            verbosity: openaiOptions.textVerbosity,
          }),
        },
      }),

      // provider options:
      max_tool_calls: openaiOptions?.maxToolCalls,
      metadata: openaiOptions?.metadata,
      parallel_tool_calls: openaiOptions?.parallelToolCalls,
      previous_response_id: openaiOptions?.previousResponseId,
      store: openaiOptions?.store,
      user: openaiOptions?.user,
      instructions: openaiOptions?.instructions,
      service_tier: openaiOptions?.serviceTier,
      include,
      prompt_cache_key: openaiOptions?.promptCacheKey,
      safety_identifier: openaiOptions?.safetyIdentifier,
      top_logprobs: topLogprobs,

      // model-specific settings:
      ...(modelConfig.isReasoningModel &&
        (openaiOptions?.reasoningEffort != null || openaiOptions?.reasoningSummary != null) && {
          reasoning: {
            ...(openaiOptions?.reasoningEffort != null && {
              effort: openaiOptions.reasoningEffort,
            }),
            ...(openaiOptions?.reasoningSummary != null && {
              summary: openaiOptions.reasoningSummary,
            }),
          },
        }),
      ...(modelConfig.requiredAutoTruncation && {
        truncation: "auto",
      }),
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L350-371)
```typescript
    // Validate flex processing support
    if (openaiOptions?.serviceTier === "flex" && !modelConfig.supportsFlexProcessing) {
      warnings.push({
        type: "unsupported-setting",
        setting: "serviceTier",
        details: "flex processing is only available for o3, o4-mini, and gpt-5 models",
      })
      // Remove from args if not supported
      delete (baseArgs as any).service_tier
    }

    // Validate priority processing support
    if (openaiOptions?.serviceTier === "priority" && !modelConfig.supportsPriorityProcessing) {
      warnings.push({
        type: "unsupported-setting",
        setting: "serviceTier",
        details:
          "priority processing is only available for supported models (gpt-4, gpt-5, gpt-5-mini, o3, o4-mini) and requires Enterprise access. gpt-5-nano is not supported",
      })
      // Remove from args if not supported
      delete (baseArgs as any).service_tier
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L413-493)
```typescript
        z.object({
          id: z.string(),
          created_at: z.number(),
          error: z
            .object({
              code: z.string(),
              message: z.string(),
            })
            .nullish(),
          model: z.string(),
          output: z.array(
            z.discriminatedUnion("type", [
              z.object({
                type: z.literal("message"),
                role: z.literal("assistant"),
                id: z.string(),
                content: z.array(
                  z.object({
                    type: z.literal("output_text"),
                    text: z.string(),
                    logprobs: LOGPROBS_SCHEMA.nullish(),
                    annotations: z.array(
                      z.discriminatedUnion("type", [
                        z.object({
                          type: z.literal("url_citation"),
                          start_index: z.number(),
                          end_index: z.number(),
                          url: z.string(),
                          title: z.string(),
                        }),
                        z.object({
                          type: z.literal("file_citation"),
                          file_id: z.string(),
                          filename: z.string().nullish(),
                          index: z.number().nullish(),
                          start_index: z.number().nullish(),
                          end_index: z.number().nullish(),
                          quote: z.string().nullish(),
                        }),
                        z.object({
                          type: z.literal("container_file_citation"),
                        }),
                      ]),
                    ),
                  }),
                ),
              }),
              webSearchCallItem,
              fileSearchCallItem,
              codeInterpreterCallItem,
              imageGenerationCallItem,
              localShellCallItem,
              z.object({
                type: z.literal("function_call"),
                call_id: z.string(),
                name: z.string(),
                arguments: z.string(),
                id: z.string(),
              }),
              z.object({
                type: z.literal("computer_call"),
                id: z.string(),
                status: z.string().optional(),
              }),
              z.object({
                type: z.literal("reasoning"),
                id: z.string(),
                encrypted_content: z.string().nullish(),
                summary: z.array(
                  z.object({
                    type: z.literal("summary_text"),
                    text: z.string(),
                  }),
                ),
              }),
            ]),
          ),
          service_tier: z.string().nullish(),
          incomplete_details: z.object({ reason: z.string() }).nullish(),
          usage: usageSchema,
        }),
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L518-732)
```typescript
    for (const part of response.output) {
      switch (part.type) {
        case "reasoning": {
          // when there are no summary parts, we need to add an empty reasoning part:
          if (part.summary.length === 0) {
            part.summary.push({ type: "summary_text", text: "" })
          }

          for (const summary of part.summary) {
            content.push({
              type: "reasoning" as const,
              text: summary.text,
              providerMetadata: {
                openai: {
                  itemId: part.id,
                  reasoningEncryptedContent: part.encrypted_content ?? null,
                },
              },
            })
          }
          break
        }

        case "image_generation_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "image_generation",
            input: "{}",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "image_generation",
            result: {
              result: part.result,
            } satisfies z.infer<typeof imageGenerationOutputSchema>,
            providerExecuted: true,
          })

          break
        }

        case "local_shell_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.call_id,
            toolName: "local_shell",
            input: JSON.stringify({ action: part.action } satisfies z.infer<typeof localShellInputSchema>),
            providerMetadata: {
              openai: {
                itemId: part.id,
              },
            },
          })

          break
        }

        case "message": {
          for (const contentPart of part.content) {
            if (options.providerOptions?.openai?.logprobs && contentPart.logprobs) {
              logprobs.push(contentPart.logprobs)
            }

            content.push({
              type: "text",
              text: contentPart.text,
              providerMetadata: {
                openai: {
                  itemId: part.id,
                },
              },
            })

            for (const annotation of contentPart.annotations) {
              if (annotation.type === "url_citation") {
                content.push({
                  type: "source",
                  sourceType: "url",
                  id: this.config.generateId?.() ?? generateId(),
                  url: annotation.url,
                  title: annotation.title,
                })
              } else if (annotation.type === "file_citation") {
                content.push({
                  type: "source",
                  sourceType: "document",
                  id: this.config.generateId?.() ?? generateId(),
                  mediaType: "text/plain",
                  title: annotation.quote ?? annotation.filename ?? "Document",
                  filename: annotation.filename ?? annotation.file_id,
                })
              }
            }
          }

          break
        }

        case "function_call": {
          hasFunctionCall = true

          content.push({
            type: "tool-call",
            toolCallId: part.call_id,
            toolName: part.name,
            input: part.arguments,
            providerMetadata: {
              openai: {
                itemId: part.id,
              },
            },
          })
          break
        }

        case "web_search_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: webSearchToolName ?? "web_search",
            input: JSON.stringify({ action: part.action }),
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: webSearchToolName ?? "web_search",
            result: { status: part.status },
            providerExecuted: true,
          })

          break
        }

        case "computer_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "computer_use",
            input: "",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "computer_use",
            result: {
              type: "computer_use_tool_result",
              status: part.status || "completed",
            },
            providerExecuted: true,
          })
          break
        }

        case "file_search_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "file_search",
            input: "{}",
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "file_search",
            result: {
              queries: part.queries,
              results:
                part.results?.map((result) => ({
                  attributes: result.attributes,
                  fileId: result.file_id,
                  filename: result.filename,
                  score: result.score,
                  text: result.text,
                })) ?? null,
            } satisfies z.infer<typeof fileSearchOutputSchema>,
            providerExecuted: true,
          })
          break
        }

        case "code_interpreter_call": {
          content.push({
            type: "tool-call",
            toolCallId: part.id,
            toolName: "code_interpreter",
            input: JSON.stringify({
              code: part.code,
              containerId: part.container_id,
            } satisfies z.infer<typeof codeInterpreterInputSchema>),
            providerExecuted: true,
          })

          content.push({
            type: "tool-result",
            toolCallId: part.id,
            toolName: "code_interpreter",
            result: {
              outputs: part.outputs,
            } satisfies z.infer<typeof codeInterpreterOutputSchema>,
            providerExecuted: true,
          })
          break
        }
      }
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts (L818-831)
```typescript
    // Track reasoning by output_index instead of item_id
    // GitHub Copilot rotates encrypted item IDs on every event
    const activeReasoning: Record<
      number,
      {
        canonicalId: string // the item.id from output_item.added
        encryptedContent?: string | null
        summaryParts: number[]
      }
    > = {}

    // Track current active reasoning output_index for correlating summary events
    let currentReasoningOutputIndex: number | null = null

```

**File:** packages/opencode/src/provider/sdk/copilot/responses/convert-to-openai-responses-input.ts (L21-295)
```typescript
export async function convertToOpenAIResponsesInput({
  prompt,
  systemMessageMode,
  fileIdPrefixes,
  store,
  hasLocalShellTool = false,
}: {
  prompt: LanguageModelV2Prompt
  systemMessageMode: "system" | "developer" | "remove"
  fileIdPrefixes?: readonly string[]
  store: boolean
  hasLocalShellTool?: boolean
}): Promise<{
  input: OpenAIResponsesInput
  warnings: Array<LanguageModelV2CallWarning>
}> {
  const input: OpenAIResponsesInput = []
  const warnings: Array<LanguageModelV2CallWarning> = []

  for (const { role, content } of prompt) {
    switch (role) {
      case "system": {
        switch (systemMessageMode) {
          case "system": {
            input.push({ role: "system", content })
            break
          }
          case "developer": {
            input.push({ role: "developer", content })
            break
          }
          case "remove": {
            warnings.push({
              type: "other",
              message: "system messages are removed for this model",
            })
            break
          }
          default: {
            const _exhaustiveCheck: never = systemMessageMode
            throw new Error(`Unsupported system message mode: ${_exhaustiveCheck}`)
          }
        }
        break
      }

      case "user": {
        input.push({
          role: "user",
          content: content.map((part, index) => {
            switch (part.type) {
              case "text": {
                return { type: "input_text", text: part.text }
              }
              case "file": {
                if (part.mediaType.startsWith("image/")) {
                  const mediaType = part.mediaType === "image/*" ? "image/jpeg" : part.mediaType

                  return {
                    type: "input_image",
                    ...(part.data instanceof URL
                      ? { image_url: part.data.toString() }
                      : typeof part.data === "string" && isFileId(part.data, fileIdPrefixes)
                        ? { file_id: part.data }
                        : {
                            image_url: `data:${mediaType};base64,${convertToBase64(part.data)}`,
                          }),
                    detail: part.providerOptions?.openai?.imageDetail,
                  }
                } else if (part.mediaType === "application/pdf") {
                  if (part.data instanceof URL) {
                    return {
                      type: "input_file",
                      file_url: part.data.toString(),
                    }
                  }
                  return {
                    type: "input_file",
                    ...(typeof part.data === "string" && isFileId(part.data, fileIdPrefixes)
                      ? { file_id: part.data }
                      : {
                          filename: part.filename ?? `part-${index}.pdf`,
                          file_data: `data:application/pdf;base64,${convertToBase64(part.data)}`,
                        }),
                  }
                } else {
                  throw new UnsupportedFunctionalityError({
                    functionality: `file part media type ${part.mediaType}`,
                  })
                }
              }
            }
          }),
        })

        break
      }

      case "assistant": {
        const reasoningMessages: Record<string, OpenAIResponsesReasoning> = {}
        const toolCallParts: Record<string, LanguageModelV2ToolCallPart> = {}

        for (const part of content) {
          switch (part.type) {
            case "text": {
              input.push({
                role: "assistant",
                content: [{ type: "output_text", text: part.text }],
                id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
              })
              break
            }
            case "tool-call": {
              toolCallParts[part.toolCallId] = part

              if (part.providerExecuted) {
                break
              }

              if (hasLocalShellTool && part.toolName === "local_shell") {
                const parsedInput = localShellInputSchema.parse(part.input)
                input.push({
                  type: "local_shell_call",
                  call_id: part.toolCallId,
                  id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
                  action: {
                    type: "exec",
                    command: parsedInput.action.command,
                    timeout_ms: parsedInput.action.timeoutMs,
                    user: parsedInput.action.user,
                    working_directory: parsedInput.action.workingDirectory,
                    env: parsedInput.action.env,
                  },
                })

                break
              }

              input.push({
                type: "function_call",
                call_id: part.toolCallId,
                name: part.toolName,
                arguments: JSON.stringify(part.input),
                id: (part.providerOptions?.openai?.itemId as string) ?? undefined,
              })
              break
            }

            // assistant tool result parts are from provider-executed tools:
            case "tool-result": {
              if (store) {
                // use item references to refer to tool results from built-in tools
                input.push({ type: "item_reference", id: part.toolCallId })
              } else {
                warnings.push({
                  type: "other",
                  message: `Results for OpenAI tool ${part.toolName} are not sent to the API when store is false`,
                })
              }

              break
            }

            case "reasoning": {
              const providerOptions = await parseProviderOptions({
                provider: "copilot",
                providerOptions: part.providerOptions,
                schema: openaiResponsesReasoningProviderOptionsSchema,
              })

              const reasoningId = providerOptions?.itemId

              if (reasoningId != null) {
                const reasoningMessage = reasoningMessages[reasoningId]

                if (store) {
                  if (reasoningMessage === undefined) {
                    // use item references to refer to reasoning (single reference)
                    input.push({ type: "item_reference", id: reasoningId })

                    // store unused reasoning message to mark id as used
                    reasoningMessages[reasoningId] = {
                      type: "reasoning",
                      id: reasoningId,
                      summary: [],
                    }
                  }
                } else {
                  const summaryParts: Array<{
                    type: "summary_text"
                    text: string
                  }> = []

                  if (part.text.length > 0) {
                    summaryParts.push({
                      type: "summary_text",
                      text: part.text,
                    })
                  } else if (reasoningMessage !== undefined) {
                    warnings.push({
                      type: "other",
                      message: `Cannot append empty reasoning part to existing reasoning sequence. Skipping reasoning part: ${JSON.stringify(part)}.`,
                    })
                  }

                  if (reasoningMessage === undefined) {
                    reasoningMessages[reasoningId] = {
                      type: "reasoning",
                      id: reasoningId,
                      encrypted_content: providerOptions?.reasoningEncryptedContent,
                      summary: summaryParts,
                    }
                    input.push(reasoningMessages[reasoningId])
                  } else {
                    reasoningMessage.summary.push(...summaryParts)
                  }
                }
              } else {
                warnings.push({
                  type: "other",
                  message: `Non-OpenAI reasoning parts are not supported. Skipping reasoning part: ${JSON.stringify(part)}.`,
                })
              }
              break
            }
          }
        }

        break
      }

      case "tool": {
        for (const part of content) {
          const output = part.output

          if (hasLocalShellTool && part.toolName === "local_shell" && output.type === "json") {
            input.push({
              type: "local_shell_call_output",
              call_id: part.toolCallId,
              output: localShellOutputSchema.parse(output.value).output,
            })
            break
          }

          let contentValue: string
          switch (output.type) {
            case "text":
            case "error-text":
              contentValue = output.value
              break
            case "content":
            case "json":
            case "error-json":
              contentValue = JSON.stringify(output.value)
              break
          }

          input.push({
            type: "function_call_output",
            call_id: part.toolCallId,
            output: contentValue,
          })
        }

        break
      }

      default: {
        const _exhaustiveCheck: never = role
        throw new Error(`Unsupported role: ${_exhaustiveCheck}`)
      }
    }
  }

  return { input, warnings }
```

**File:** packages/opencode/src/provider/transform.ts (L21-25)
```typescript
  function sdkKey(npm: string): string | undefined {
    switch (npm) {
      case "@ai-sdk/github-copilot":
        return "copilot"
      case "@ai-sdk/openai":
```

**File:** packages/opencode/src/provider/transform.ts (L367-391)
```typescript
      case "@ai-sdk/github-copilot":
        if (model.id.includes("gemini")) {
          // currently github copilot only returns thinking
          return {}
        }
        if (model.id.includes("claude")) {
          return {
            thinking: { thinking_budget: 4000 },
          }
        }
        const copilotEfforts = iife(() => {
          if (id.includes("5.1-codex-max") || id.includes("5.2") || id.includes("5.3"))
            return [...WIDELY_SUPPORTED_EFFORTS, "xhigh"]
          return WIDELY_SUPPORTED_EFFORTS
        })
        return Object.fromEntries(
          copilotEfforts.map((effort) => [
            effort,
            {
              reasoningEffort: effort,
              reasoningSummary: "auto",
              include: ["reasoning.encrypted_content"],
            },
          ]),
        )
```

**File:** packages/opencode/src/provider/transform.ts (L816-824)
```typescript
    if (providerID.includes("github-copilot") && error.statusCode === 403) {
      return "Please reauthenticate with the copilot provider to ensure your credentials work properly with OpenCode."
    }
    if (providerID.includes("github-copilot") && message.includes("The requested model is not supported")) {
      return (
        message +
        "\n\nMake sure the model is enabled in your copilot settings: https://github.com/settings/copilot/features"
      )
    }
```

**File:** packages/opencode/src/provider/sdk/copilot/copilot-provider.ts (L49-97)
```typescript
/**
 * Create an OpenAI Compatible provider instance.
 */
export function createOpenaiCompatible(options: OpenaiCompatibleProviderSettings = {}): OpenaiCompatibleProvider {
  const baseURL = withoutTrailingSlash(options.baseURL ?? "https://api.openai.com/v1")

  if (!baseURL) {
    throw new Error("baseURL is required")
  }

  // Merge headers: defaults first, then user overrides
  const headers = {
    // Default OpenAI Compatible headers (can be overridden by user)
    ...(options.apiKey && { Authorization: `Bearer ${options.apiKey}` }),
    ...options.headers,
  }

  const getHeaders = () => withUserAgentSuffix(headers, `ai-sdk/openai-compatible/${VERSION}`)

  const createChatModel = (modelId: OpenaiCompatibleModelId) => {
    return new OpenAICompatibleChatLanguageModel(modelId, {
      provider: `${options.name ?? "openai-compatible"}.chat`,
      headers: getHeaders,
      url: ({ path }) => `${baseURL}${path}`,
      fetch: options.fetch,
    })
  }

  const createResponsesModel = (modelId: OpenaiCompatibleModelId) => {
    return new OpenAIResponsesLanguageModel(modelId, {
      provider: `${options.name ?? "openai-compatible"}.responses`,
      headers: getHeaders,
      url: ({ path }) => `${baseURL}${path}`,
      fetch: options.fetch,
    })
  }

  const createLanguageModel = (modelId: OpenaiCompatibleModelId) => createChatModel(modelId)

  const provider = function (modelId: OpenaiCompatibleModelId) {
    return createChatModel(modelId)
  }

  provider.languageModel = createLanguageModel
  provider.chat = createChatModel
  provider.responses = createResponsesModel

  return provider as OpenaiCompatibleProvider
}
```
