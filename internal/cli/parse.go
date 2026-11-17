package cli

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/neko233-com/DownloadX/internal/engine"
)

// Options is the parsed command line.
type Options struct {
	engine.Task
	Input       string
	Sources     []string
	JSON        bool
	Quiet       bool
	ShowHelp    bool
	ShowVersion bool
}

var (
	valueFlags = map[string]bool{
		"output": true, "dir": true, "connections": true, "input": true,
		"header": true, "cookie": true, "referer": true, "user-agent": true,
		"user": true, "proxy": true, "retry": true, "timeout": true,
		"limit": true, "type": true, "hls-quality": true, "bt-port": true,
		"ffmpeg": true,
	}
	boolFlags = map[string]bool{
		"no-resume": true, "insecure": true, "json": true, "quiet": true,
		"seed": true, "version": true, "help": true,
	}
	shortNames = map[string]string{
		"o": "output", "d": "dir", "x": "connections", "i": "input",
		"H": "header", "u": "user", "q": "quiet", "v": "version", "h": "help",
	}
)

func defaults() *Options {
	return &Options{Task: engine.Task{
		Dir:         ".",
		Connections: engine.DefaultConnections,
		Retries:     3,
		Timeout:     30 * time.Second,
		Type:        engine.TypeAuto,
		HLSQuality:  "best",
		FFmpeg:      "ffmpeg",
		Headers:     map[string]string{},
	}}
}

// Parse parses arguments. Flags may appear before or after sources.
func Parse(args []string) (*Options, error) {
	o := defaults()
	endFlags := false

	for i := 0; i < len(args); {
		a := args[i]
		if endFlags || a == "-" || !strings.HasPrefix(a, "-") {
			o.Sources = append(o.Sources, a)
			i++
			continue
		}
		if a == "--" {
			endFlags = true
			i++
			continue
		}

		name := a
		val := ""
		hasVal := false
		if strings.HasPrefix(a, "--") {
			name = a[2:]
			if j := strings.IndexByte(name, '='); j >= 0 {
				val, name = name[j+1:], name[:j]
				hasVal = true
			}
		} else {
			name = a[1:]
			if j := strings.IndexByte(name, '='); j >= 0 {
				val, name = name[j+1:], name[:j]
				hasVal = true
			}
		}
		if short, ok := shortNames[name]; ok {
			name = short
		}

		if boolFlags[name] {
			if hasVal {
				return nil, fmt.Errorf("flag -%s does not take a value", name)
			}
			switch name {
			case "no-resume":
				o.NoResume = true
			case "insecure":
				o.Insecure = true
			case "json":
				o.JSON = true
			case "quiet":
				o.Quiet = true
			case "seed":
				o.Seed = true
			case "version":
				o.ShowVersion = true
			case "help":
				o.ShowHelp = true
			}
			i++
			continue
		}
		if !valueFlags[name] {
			return nil, fmt.Errorf("unknown flag %q", a)
		}
		if !hasVal {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag -%s needs a value", name)
			}
			val = args[i+1]
			i += 2
		} else {
			i++
		}
		if err := o.setValue(name, val); err != nil {
			return nil, err
		}
	}

	if !o.ShowHelp && !o.ShowVersion && o.Input != "" {
		if err := o.addInputFile(); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (o *Options) setValue(name, val string) error {
	switch name {
	case "output":
		o.Dest = val
	case "dir":
		o.Dir = val
	case "connections":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > engine.MaxConnections {
			return fmt.Errorf("connections must be between 1 and %d", engine.MaxConnections)
		}
		o.Connections = n
	case "input":
		o.Input = val
	case "header":
		k, v, ok := strings.Cut(val, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("invalid header %q (want \"Name: value\")", val)
		}
		o.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
	case "cookie":
		o.Cookie = val
	case "referer":
		o.Referer = val
	case "user-agent":
		o.UserAgent = val
	case "user":
		user, pass, _ := strings.Cut(val, ":")
		if user == "" {
			return fmt.Errorf("user must not be empty")
		}
		o.Username, o.Password = user, pass
	case "proxy":
		o.Proxy = val
	case "retry":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("retry must be between 0 and 100")
		}
		o.Retries = n
	case "timeout":
		d, err := time.ParseDuration(val)
		if err != nil || d <= 0 {
			return fmt.Errorf("invalid timeout %q", val)
		}
		o.Timeout = d
	case "limit":
		n, err := parseRate(val)
		if err != nil {
			return err
		}
		o.Limit = n
	case "type":
		t, err := engine.ParseType(val)
		if err != nil {
			return err
		}
		o.Type = t
	case "hls-quality":
		q := strings.ToLower(strings.TrimSpace(val))
		if q != "best" && q != "worst" {
			if n, err := strconv.Atoi(q); err != nil || n < 0 {
				return fmt.Errorf("hls-quality must be best, worst or a variant index")
			}
		}
		o.HLSQuality = q
	case "bt-port":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("bt-port must be between 0 and 65535")
		}
		o.BTPort = n
	case "ffmpeg":
		o.FFmpeg = val
	}
	return nil
}

func (o *Options) addInputFile() error {
	data, err := os.ReadFile(o.Input)
	if err != nil {
		return fmt.Errorf("read input file: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		o.Sources = append(o.Sources, line)
	}
	return nil
}

func parseRate(val string) (int64, error) {
	orig := val
	s := strings.ToLower(strings.TrimSpace(val))
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSuffix(s, "b")

	mult := float64(1)
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'k':
			mult, s = 1024, s[:len(s)-1]
		case 'm':
			mult, s = 1024*1024, s[:len(s)-1]
		case 'g':
			mult, s = 1024*1024*1024, s[:len(s)-1]
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("invalid rate %q (examples: 5M, 800K, 1048576)", orig)
	}
	if f == 0 {
		return 0, nil
	}
	v := f * mult
	if v >= math.MaxInt64 {
		return 0, fmt.Errorf("rate %q is too large", orig)
	}
	return int64(v), nil
}
