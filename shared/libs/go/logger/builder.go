package logger

import "fmt"

// LogOutputConfig describes a single log output destination.
type LogOutputConfig struct {
	Type    string `yaml:"type"`              // "stdout" or "syslog"
	Network string `yaml:"network,omitempty"` // syslog: "udp" or "tcp"
	Address string `yaml:"address,omitempty"` // syslog: "host:port"
	Tag     string `yaml:"tag,omitempty"`     // syslog: tag prefix
}

// BuildFromConfig creates a DefaultLogger from LogConfig.
// If no outputs are specified, defaults to stdout.
func BuildFromConfig(level Level, outputs []LogOutputConfig) (*DefaultLogger, error) {
	if len(outputs) == 0 {
		return NewDefault(level), nil
	}

	hasStdout := false
	for _, o := range outputs {
		if o.Type == "stdout" {
			hasStdout = true
			break
		}
	}

	var writers []LogWriter
	for _, o := range outputs {
		switch o.Type {
		case "stdout":
			writers = append(writers, NewStdoutWriter())
		case "syslog":
			sw, err := NewSyslogWriter(o.Network, o.Address, o.Tag, hasStdout)
			if err != nil {
				// Close already opened writers to clean up resources
				for _, w := range writers {
					_ = w.Close()
				}
				return nil, fmt.Errorf("create syslog writer: %w", err)
			}
			writers = append(writers, sw)
		default:
			// Close already opened writers to clean up resources
			for _, w := range writers {
				_ = w.Close()
			}
			return nil, fmt.Errorf("unknown log output type: %q", o.Type)
		}
	}

	var writer LogWriter
	if len(writers) == 1 {
		writer = writers[0]
	} else {
		// NewMultiWriter expects ...LogWriter, convert slice to variadic
		var lws []LogWriter
		for _, w := range writers {
			lws = append(lws, w)
		}
		writer = NewMultiWriter(lws...)
	}

	return NewDefaultWithOptions(level, &TextFormatter{}, writer), nil
}
