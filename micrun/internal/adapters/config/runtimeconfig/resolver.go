package runtimeconfig

import (
	"fmt"
	"reflect"
	"strings"

	configstack "micrun/internal/adapters/config/configstack"
	"micrun/internal/adapters/config/oci"
	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
	"micrun/internal/support/validation"

	"github.com/containerd/typeurl/v2"
)

type configPathSource string

const (
	configPathSourceAnnotation configPathSource = "annotation"
	configPathSourceOptions    configPathSource = "options"
	configPathSourceEnv        configPathSource = "env"
)

type configPathCandidate struct {
	Path   string
	Source configPathSource
}

func (c configPathCandidate) Found() bool {
	return c.Path != ""
}

type configPathResolver struct {
	Source  configPathSource
	Resolve func() (string, error)
}

type Resolver struct {
	hostProfile oci.HostProfile
}

func NewResolver(hostProfile oci.HostProfile) Resolver {
	return Resolver{
		hostProfile: hostProfile,
	}
}

func (r Resolver) Resolve(current *oci.RuntimeConfig, req ports.TaskCreateRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	stack := oci.NewRuntimeStackWithHost(r.host())
	return r.resolveWithStack(stack, current, req, annotations)
}

func (r Resolver) resolveWithStack(stack *oci.RuntimeStack, current *oci.RuntimeConfig, req ports.TaskCreateRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	if current != nil {
		return current, nil
	}

	candidate, err := resolveConfigPathCandidate(req, annotations)
	if err != nil {
		return nil, err
	}

	if candidate.Found() {
		parsed, err := loadConfigFromFile(candidate.Path, r.host())
		if err != nil {
			if candidate.Source == configPathSourceEnv {
				log.Warnf("failed to load runtime config from %s (env): %v; using defaults.", candidate.Path, err)
				stack.Replace(nil)
			} else {
				return nil, fmt.Errorf("failed to load runtime config from %s (%s): %w", candidate.Path, candidate.Source, err)
			}
		} else {
			stack.Replace(parsed)
		}
	} else {
		files, err := configstack.DiscoverMicrunConfigFiles()
		if err != nil {
			log.Warnf("micrun config discovery failed: %v", err)
		}
		if err := stack.ApplyMicrunFiles(files); err != nil {
			log.Warnf("micrun config apply failed: %v", err)
		}
	}

	stack.ApplyAnnotations(annotations)
	cfg := stack.Config()
	return cfg, nil
}

func (r Resolver) host() oci.HostProfile {
	return r.hostProfile
}

func resolveConfigPathCandidate(req ports.TaskCreateRequest, annotations map[string]string) (configPathCandidate, error) {
	return firstConfigPathCandidate(configPathResolvers(req, annotations))
}

func configPathResolvers(req ports.TaskCreateRequest, annotations map[string]string) []configPathResolver {
	return []configPathResolver{
		{
			Source: configPathSourceAnnotation,
			Resolve: func() (string, error) {
				return oci.GetSandboxConfigPath(annotations), nil
			},
		},
		{
			Source: configPathSourceOptions,
			Resolve: func() (string, error) {
				if !hasRuntimeOptions(req.Options) {
					return "", nil
				}
				return getConfigPathFromOptions(req.Options)
			},
		},
		{
			Source: configPathSourceEnv,
			Resolve: func() (string, error) {
				return configstack.FirstNonEmptyEnv(defs.MicrunConfEnv), nil
			},
		},
	}
}

func firstConfigPathCandidate(resolvers []configPathResolver) (configPathCandidate, error) {
	for _, resolver := range resolvers {
		if resolver.Resolve == nil {
			continue
		}
		path, err := resolver.Resolve()
		if err != nil {
			return configPathCandidate{}, err
		}
		if path, ok := nonEmptyConfigPath(path); ok {
			return configPathCandidate{Path: path, Source: resolver.Source}, nil
		}
	}
	return configPathCandidate{}, nil
}

func hasRuntimeOptions(options typeurl.Any) bool {
	return !validation.IsNil(options)
}

func loadConfigFromFile(configPath string, hostProfile oci.HostProfile) (*oci.RuntimeConfig, error) {
	file, err := configstack.MicrunConfigFileFromPath(configPath)
	if err != nil {
		return nil, err
	}

	cfg := oci.NewRuntimeConfigWithHost(hostProfile)
	switch file.Format {
	case configstack.FormatINI:
		if err := cfg.ParseRuntimeFromINI(file.Path); err != nil {
			return nil, err
		}
	case configstack.FormatTOML:
		if err := cfg.ParseRuntimeFromToml(file.Path); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported micrun config format %v for %s", file.Format, file.Path)
	}
	return cfg, nil
}

func getConfigPathFromOptions(options typeurl.Any) (string, error) {
	v, err := typeurl.UnmarshalAny(options)
	if err != nil {
		return "", err
	}

	if p, ok := configPathFromDecodedOptions(v); ok {
		return p, nil
	}

	return "", nil
}

type configPathGetter interface {
	GetConfigPath() string
}

func configPathFromDecodedOptions(v any) (string, bool) {
	if v == nil {
		return "", false
	}

	if getter, ok := v.(configPathGetter); ok {
		if path, ok := nonEmptyConfigPath(getter.GetConfigPath()); ok {
			return path, true
		}
	}

	return configPathFromStructField(v)
}

func configPathFromStructField(v any) (string, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return "", false
	}

	field := rv.FieldByName("ConfigPath")
	if field.IsValid() && field.Kind() == reflect.String {
		return nonEmptyConfigPath(field.String())
	}
	return "", false
}

func nonEmptyConfigPath(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
