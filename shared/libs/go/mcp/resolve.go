package mcp

import (
	"fmt"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
)

// ResolvedServer is MCPServerConfig with secrets resolved.
type ResolvedServer struct {
	toolconfig.MCPServerConfig
}

// ResolveServerSecrets copies cfg and resolves vault:// values in env/headers.
func ResolveServerSecrets(cfg toolconfig.MCPServerConfig, r SecretResolver) (ResolvedServer, error) {
	out := ResolvedServer{MCPServerConfig: cfg}
	var err error
	out.Env, err = resolveMap(cfg.Env, r)
	if err != nil {
		return ResolvedServer{}, err
	}
	out.Headers, err = resolveMap(cfg.Headers, r)
	if err != nil {
		return ResolvedServer{}, err
	}
	return out, nil
}

func resolveMap(in map[string]string, r SecretResolver) (map[string]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if strings.HasPrefix(v, "vault://") {
			if r == nil {
				return nil, fmt.Errorf("vault reference %q but no resolver configured", v)
			}
			resolved, err := r.Resolve(v)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", k, err)
			}
			out[k] = resolved
			continue
		}
		out[k] = v
	}
	return out, nil
}
