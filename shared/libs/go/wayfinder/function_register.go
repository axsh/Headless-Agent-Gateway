package wayfinder

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
	"github.com/google/uuid"
)

// RegisterClientFunctions registers Client API function schemas.
// Handlers emit FunctionCallError so AgentCore can bridge to the client.
func RegisterClientFunctions(reg *tools.Registry, fns map[string]toolconfig.FunctionConfig) error {
	for name, cfg := range fns {
		var schema map[string]any
		if err := json.Unmarshal(cfg.Parameters, &schema); err != nil {
			return fmt.Errorf("function %s parameters: %w", name, err)
		}
		fnName := name
		reg.Register(fnName, cfg.Description, schema, func(_ context.Context, input map[string]any) (string, error) {
			return "", &tools.FunctionCallError{Req: tools.FunctionCallRequest{
				CallID:    uuid.NewString(),
				Name:      fnName,
				Arguments: input,
			}}
		})
	}
	return nil
}
