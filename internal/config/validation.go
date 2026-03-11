package config

import "fmt"

// ValidateDispatchConfig checks that the service config has all required fields
// for polling and dispatching work. This is the scheduler preflight check.
func ValidateDispatchConfig(cfg *ServiceConfig) error {
	if cfg.TrackerKind == "" {
		return fmt.Errorf("validation failed: tracker.kind is required")
	}
	switch cfg.TrackerKind {
	case "jira":
		if cfg.TrackerEndpoint == "" {
			return fmt.Errorf("validation failed: tracker.endpoint is required")
		}
		if cfg.TrackerAPIToken == "" {
			return fmt.Errorf("validation failed: tracker.api_token is missing")
		}
		if cfg.TrackerEmail == "" {
			return fmt.Errorf("validation failed: tracker.email is missing")
		}
		if cfg.TrackerProjectKey == "" {
			return fmt.Errorf("validation failed: tracker.project_key is required")
		}
	case "github":
		if cfg.TrackerOwner == "" {
			return fmt.Errorf("validation failed: tracker.owner is required")
		}
		if cfg.TrackerRepo == "" {
			return fmt.Errorf("validation failed: tracker.repo is required")
		}
		if cfg.TrackerAPIToken == "" {
			return fmt.Errorf("validation failed: tracker.api_token is missing")
		}
	default:
		return fmt.Errorf("validation failed: unsupported tracker.kind %q", cfg.TrackerKind)
	}
	if cfg.AgentImage == "" {
		return fmt.Errorf("validation failed: agent.image is required")
	}
	return nil
}
