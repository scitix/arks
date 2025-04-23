package quota

import (
	"encoding/json"
	"testing"

	jsoniter "github.com/json-iterator/go"
	"github.com/openai/openai-go"
)

var testJSON = []byte(`{
  "model": "gpt-4-turbo-preview",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant."
    },
    {
      "role": "user",
      "content": "Explain quantum computing in simple terms",
      "name": "user123",
      "function_call": {
        "name": "get_current_weather",
        "arguments": "{\"location\":\"Boston,MA\"}"
      }
    }
  ],
  "functions": [
    {
      "name": "get_current_weather",
      "description": "Get the current weather in a given location",
      "parameters": {
        "type": "object",
        "properties": {
          "location": {
            "type": "string",
            "description": "The city and state, e.g. San Francisco, CA"
          },
          "unit": {
            "type": "string",
            "enum": ["celsius", "fahrenheit"]
          }
        },
        "required": ["location"]
      }
    }
  ],
  "temperature": 0.7,
  "top_p": 0.95,
  "n": 3,
  "stream": true,
  "stream_options": {
    "include_usage": false
  },
  "max_tokens": 2048,
  "presence_penalty": 0.5,
  "frequency_penalty": 0.5,
  "logit_bias": {
    "50256": -100,
    "198": -10,
    "628": 5
  },
  "user": "user_123456",
  "response_format": {
    "type": "json_object"
  },
  "seed": 42,
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_current_time",
        "description": "Get the current time in a given timezone",
        "parameters": {
          "type": "object",
          "properties": {
            "timezone": {
              "type": "string",
              "description": "The timezone, e.g. America/New_York"
            }
          },
          "required": ["timezone"]
        }
      }
    }
  ],
  "parallel_tool_calls": true,
  "experimental": {
    "rag": {
      "sources": [
        {
          "url": "https://example.com/doc1",
          "title": "Quantum Computing Basics"
        }
      ]
    },
    "fallback_models": ["gpt-4", "gpt-3.5-turbo"]
  }
}`)

type ModelSimple struct {
	Model string `json:"model"`
}

func BenchmarkStructStd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var model openai.ChatCompletionNewParams
		if err := json.Unmarshal(testJSON, &model); err != nil {
			b.Fatal(err)
		}
		_ = model.Model
	}
}

func BenchmarkMapStd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var data map[string]interface{}
		if err := json.Unmarshal(testJSON, &data); err != nil {
			b.Fatal(err)
		}
		_ = data["model"]
	}
}

func BenchmarkStructJsoniter(b *testing.B) {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	for i := 0; i < b.N; i++ {
		var model openai.ChatCompletionNewParams
		if err := json.Unmarshal(testJSON, &model); err != nil {
			b.Fatal(err)
		}
		_ = model.Model
	}
}

func BenchmarkPartialStructJsoniter(b *testing.B) {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	for i := 0; i < b.N; i++ {
		var model ModelSimple
		if err := json.Unmarshal(testJSON, &model); err != nil {
			b.Fatal(err)
		}
		_ = model.Model
	}
}

func BenchmarkMapJsoniter(b *testing.B) {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	for i := 0; i < b.N; i++ {
		var data map[string]interface{}
		if err := json.Unmarshal(testJSON, &data); err != nil {
			b.Fatal(err)
		}
		_ = data["model"]
	}
}

func BenchmarkPartialParse(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var model ModelSimple
		if err := json.Unmarshal(testJSON, &model); err != nil {
			b.Fatal(err)
		}
		_ = model.Model
	}
}
