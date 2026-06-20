package top.gptcodex.imagestudio.android

import org.json.JSONObject

internal data class NativeHttpStreamResultSnapshot(
    val imageB64: String,
    val revisedPrompt: String,
    val sourceEvent: String,
)

internal fun extractNativeHttpStreamResult(line: String): NativeHttpStreamResultSnapshot? {
    val trimmed = line.trim()
    if (!trimmed.startsWith("data: ")) return null
    val payload = trimmed.removePrefix("data: ").trim()
    if (payload.isBlank() || payload == "[DONE]") return null
    return try {
        val event = JSONObject(payload)
        when (event.optString("type")) {
            "response.output_item.done" -> {
                val item = event.optJSONObject("item")
                if (item?.optString("type") == "image_generation_call") {
                    val result = item.optString("result")
                    if (result.isNotBlank()) {
                        NativeHttpStreamResultSnapshot(
                            imageB64 = result,
                            revisedPrompt = item.optString("revised_prompt"),
                            sourceEvent = "final",
                        )
                    } else null
                } else null
            }
            "image_generation.completed", "image_edit.completed" -> {
                val result = event.optString("b64_json")
                if (result.isNotBlank()) {
                    NativeHttpStreamResultSnapshot(
                        imageB64 = result,
                        revisedPrompt = "",
                        sourceEvent = "images_api",
                    )
                } else null
            }
            else -> null
        }
    } catch (_: Exception) {
        null
    }
}

internal fun buildNativeHttpStreamProgressPayload(line: String): Any? {
    val trimmed = line.trim()
    if (!trimmed.startsWith("data: ")) {
        return mapOf("line" to line)
    }
    val payload = trimmed.removePrefix("data: ").trim()
    if (payload.isBlank() || payload == "[DONE]") {
        return mapOf("line" to line)
    }
    return try {
        val event = JSONObject(payload)
        when (event.optString("type")) {
            "response.image_generation_call.partial_image" -> mapOf(
                "event" to mapOf(
                    "type" to "response.image_generation_call.partial_image",
                    "partial_image_b64" to event.optString("partial_image_b64"),
                    "revised_prompt" to event.optString("revised_prompt"),
                    "partial_image_index" to if (event.has("partial_image_index")) event.optInt("partial_image_index", -1) else -1,
                ),
            )
            "image_generation.partial_image", "image_edit.partial_image" -> mapOf(
                "event" to mapOf(
                    "type" to event.optString("type"),
                    "b64_json" to event.optString("b64_json"),
                    "partial_image_index" to if (event.has("partial_image_index")) event.optInt("partial_image_index", -1) else -1,
                ),
            )
            "response.output_item.done" -> {
                val item = event.optJSONObject("item")
                if (item?.optString("type") == "image_generation_call" && item.optString("result").isNotBlank()) {
                    null
                } else {
                    mapOf("line" to line)
                }
            }
            "image_generation.completed", "image_edit.completed" -> {
                if (event.optString("b64_json").isNotBlank()) null else mapOf("line" to line)
            }
            else -> mapOf("line" to line)
        }
    } catch (_: Exception) {
        mapOf("line" to line)
    }
}
