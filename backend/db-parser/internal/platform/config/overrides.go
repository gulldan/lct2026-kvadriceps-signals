package config

// CanOverride ограничивает хранилище настроек рабочими параметрами продукта. Адреса, контакты и секреты,
// включая будущие неизвестные ключи, читаются только из окружения: новое переопределение разрешается здесь явно.
func CanOverride(key string) bool {
	switch key {
	case "VLLM_MODEL", "VLLM_REASONING_EFFORT", "VLLM_RPM", "RAG_REASONER_MODEL",
		"LLM_RUB_PER_USD", "LLM_COST_CURRENCY", "LLM_PRICES", "LLM_PROVIDERS",
		"LLM_BUDGET_TOTAL", "LLM_PRICE_PROMPT_PER_1M", "LLM_PRICE_COMPLETION_PER_1M",
		"PUBSEARCH_ENABLED", "PUBSEARCH_RECENT_YEARS", "RAG_GEN_BREADTH",
		"RAG_GEN_QUALITY_FLOOR", "KPI_SUGGEST", "EMBEDDINGS_MODEL", "EMBEDDING_DIM",
		"RERANKER_MODEL", "RAG_PIPELINE_REVISION", "RADAR_LLM_WORKERS", "RADAR_LLM_RPM",
		"LLM_MODEL_DISPLAY_NAME":
		return true
	default:
		return false
	}
}
