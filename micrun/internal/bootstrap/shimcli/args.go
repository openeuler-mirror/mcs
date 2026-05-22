package shimcli

import (
	"strconv"
	"strings"
)

// Args is the small subset of shim command-line parsing micrun needs before
// containerd's shim package owns the full flag set.
type Args struct {
	values  map[string]string
	options map[string]struct{}
}

type Startup struct {
	BinaryName  string
	Args        []string
	ContainerID string
	Namespace   string
	parsedArgs  Args
}

func NewStartup(shimName string, argv []string) Startup {
	args := tailArgs(argv)
	parsed := ParseArgs(args)
	return Startup{
		BinaryName:  BinaryName(shimName, argv0(argv)),
		Args:        args,
		ContainerID: parsed.Value("-id", "--id"),
		Namespace:   namespaceForLogging(parsed),
		parsedArgs:  parsed,
	}
}

func (s Startup) HasOption(names ...string) bool {
	return s.parsedArgs.HasOption(names...)
}

func (s Startup) BoolOption(names ...string) bool {
	return s.parsedArgs.BoolOption(names...)
}

func ParseArgs(args []string) Args {
	values := make(map[string]string)
	options := make(map[string]struct{})
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}

		name, value, ok := strings.Cut(arg, "=")
		if ok {
			options[name] = struct{}{}
			values[name] = value
			continue
		}
		options[arg] = struct{}{}
		if i+1 >= len(args) {
			continue
		}
		if !optionConsumesValue(arg) {
			continue
		}
		values[arg] = args[i+1]
		i++
	}
	return Args{values: values, options: options}
}

func (a Args) Value(names ...string) string {
	for _, name := range names {
		if value, ok := a.values[name]; ok {
			return value
		}
	}
	return ""
}

func (a Args) HasOption(names ...string) bool {
	for _, name := range names {
		if _, ok := a.options[name]; ok {
			return true
		}
	}
	return false
}

func (a Args) BoolOption(names ...string) bool {
	for _, name := range names {
		if _, ok := a.options[name]; !ok {
			continue
		}
		value, hasValue := a.values[name]
		if !hasValue {
			return true
		}
		parsed, err := strconv.ParseBool(value)
		return err == nil && parsed
	}
	return false
}

func tailArgs(args []string) []string {
	if len(args) <= 1 {
		return nil
	}
	return args[1:]
}

func argv0(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func optionConsumesValue(name string) bool {
	switch name {
	case "-id", "--id",
		"-namespace", "--namespace",
		"-bundle", "--bundle",
		"-address", "--address",
		"-socket", "--socket":
		return true
	default:
		return false
	}
}
