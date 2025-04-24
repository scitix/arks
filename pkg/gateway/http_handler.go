package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/openai/openai-go"
	"k8s.io/klog/v2"
)

type ModelResponse struct {
	Object string         `json:"object"`
	Data   []openai.Model `json:"data"`
}

func (s *Server) handleGetModels(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	} else {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var modelList []openai.Model

	modelNames, err := s.configProvider.GetModelList(context.Background(), token)
	if err != nil {
		klog.Errorf("error in getting model list: %v", err)
		http.Error(w, "error in getting model list", http.StatusInternalServerError)
		return
	}

	for _, modelName := range modelNames {
		modelList = append(modelList, openai.Model{
			ID: modelName,
			Object: "model",
		})
	}
	response := ModelResponse{
		Object: "list",
		Data:   modelList,
	}
	jsonBytes, err := json.Marshal(response)
	if err != nil {
		klog.Errorf("error in processing model list: %v", err)
		http.Error(w, "error in processing model list", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonBytes)
}
