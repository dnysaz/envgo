package envconfig

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Load reads a .env file and returns the parsed key/value map.
// Values are loaded in order and may reference previously defined
// variables or real OS environment variables via ${VAR} expansion.
func Load(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}
	defer f.Close()

	vars := make(map[string]string)
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("env file %s line %d: expected KEY=VALUE", path, lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("env file %s line %d: empty key", path, lineNo)
		}
		value := strings.TrimSpace(line[eq+1:])
		value = unquote(value)
		value = os.Expand(value, func(name string) string {
			if v, ok := vars[name]; ok {
				return v
			}
			return os.Getenv(name)
		})
		vars[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}
	return vars, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
