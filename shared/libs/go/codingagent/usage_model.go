package codingagent

// ApplyModelAttribution sets ModelSource and optionally backfills Model from sessionModel.
// If Model is non-empty and ModelSource unset, ModelSource becomes agent (telemetry).
// If Model is empty and sessionModel is non-empty, Model and ModelSource tern_session are set.
func ApplyModelAttribution(u *TokenUsage, sessionModel string) {
	if u == nil {
		return
	}
	if u.Model != "" {
		if u.ModelSource == "" {
			u.ModelSource = ModelSourceAgent
		}
		return
	}
	if sessionModel != "" {
		u.Model = sessionModel
		u.ModelSource = ModelSourceTernSession
	}
}
