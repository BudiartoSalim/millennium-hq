package domain

import (
	"encoding/json"
	"fmt"
)

// strictFamily is what "strict": true switches on; setting any of them to
// false afterwards quietly weakens strict mode.
func strictFamily() []string {
	return []string{"noImplicitAny", "noImplicitThis", "strictNullChecks", "strictFunctionTypes",
		"strictBindCallApply", "strictPropertyInitialization", "alwaysStrict", "useUnknownInCatchVariables"}
}

// TSConfigProblems checks an effective tsconfig, as tsc --showConfig prints
// it, for weakened type checking: strict must be on and no option of the
// strict family may be switched back off.
func TSConfigProblems(showConfig []byte) ([]string, error) {
	var config struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
	}
	if err := json.Unmarshal(showConfig, &config); err != nil {
		return nil, fmt.Errorf("read the effective tsconfig: %w", err)
	}
	var problems []string
	if config.CompilerOptions["strict"] != true {
		problems = append(problems, `"strict" must be true`)
	}
	for _, option := range strictFamily() {
		if config.CompilerOptions[option] == false {
			problems = append(problems, fmt.Sprintf("%q must not be switched off", option))
		}
	}
	return problems, nil
}
