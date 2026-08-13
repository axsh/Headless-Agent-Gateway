package toolconfig

// MaskMCPServers returns a deep copy with Env and Headers values replaced by "***".
// The input map is not modified. Nil input returns nil; empty map returns empty map.
func MaskMCPServers(in map[string]MCPServerConfig) map[string]MCPServerConfig {
	if in == nil {
		return nil
	}
	out := make(map[string]MCPServerConfig, len(in))
	for name, cfg := range in {
		cp := cfg
		if cfg.Env != nil {
			cp.Env = maskStringMap(cfg.Env)
		}
		if cfg.Headers != nil {
			cp.Headers = maskStringMap(cfg.Headers)
		}
		out[name] = cp
	}
	return out
}

func maskStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k := range in {
		out[k] = "***"
	}
	return out
}
