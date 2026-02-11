package config

// MergePrecedence merges config maps from lowest to highest precedence.
func MergePrecedence(defaults, env, file, agent, cli map[string]string) map[string]string {
	merged := map[string]string{}
	apply := func(values map[string]string) {
		for key, value := range values {
			merged[key] = value
		}
	}

	apply(defaults)
	apply(env)
	apply(file)
	apply(agent)
	apply(cli)

	return merged
}

// LoadMerged loads .rai/config and merges it with env, agent, and CLI values.
func LoadMerged(baseDir string, agent, cli, defaults map[string]string) (map[string]string, error) {
	fileValues, err := Load(baseDir)
	if err != nil {
		return nil, err
	}

	return MergePrecedence(defaults, EnvValues(), fileValues, agent, cli), nil
}
