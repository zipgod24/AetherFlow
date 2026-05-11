// aetherctl is the AetherFlow CLI.
//
//	aetherctl incident submit  --file path.json
//	aetherctl incident submit  --text "Endpoint 10.0.4.17 is..."
//	aetherctl incident watch   <incident-id>
//	aetherctl corpus ingest    --source mitre --title "T1071" --file path.md
//	aetherctl config llm       --provider openai --model gpt-4o-mini --key $OPENAI_API_KEY
//
// Defaults to http://localhost:8080. Override with --gateway or AETHER_GATEWAY.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	gateway := envOr("AETHER_GATEWAY", "http://localhost:8080")
	args, gateway = extractFlag(args, "--gateway", gateway)

	var err error
	switch cmd {
	case "incident":
		err = incidentCmd(gateway, args)
	case "corpus":
		err = corpusCmd(gateway, args)
	case "config":
		err = configCmd(gateway, args)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command: %s", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`aetherctl — AetherFlow CLI

  aetherctl incident submit  [--file PATH | --text "..."]  [--severity medium]
  aetherctl incident watch   <incident-id>
  aetherctl corpus   ingest  --source SRC --title T (--file PATH | --text "...")
  aetherctl config   llm     --provider P [--model M] [--base-url U] [--key K]

Global: --gateway URL  (default $AETHER_GATEWAY or http://localhost:8080)`)
}

func incidentCmd(gateway string, args []string) error {
	if len(args) == 0 {
		return errors.New("incident <submit|watch>")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "submit":
		body, err := buildSubmitBody(rest)
		if err != nil {
			return err
		}
		return doJSON(gateway+"/v1/incidents", body, os.Stdout)
	case "watch":
		if len(rest) < 1 {
			return errors.New("incident watch <incident-id>")
		}
		return watchSSE(gateway + "/v1/events?incident=" + rest[0])
	default:
		return fmt.Errorf("unknown incident subcommand: %s", sub)
	}
}

func buildSubmitBody(args []string) (any, error) {
	var (
		file, text, severity string
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--file":
			if i+1 >= len(args) {
				return nil, errors.New("--file needs a value")
			}
			file = args[i+1]
			i++
		case "--text":
			if i+1 >= len(args) {
				return nil, errors.New("--text needs a value")
			}
			text = args[i+1]
			i++
		case "--severity":
			if i+1 >= len(args) {
				return nil, errors.New("--severity needs a value")
			}
			severity = args[i+1]
			i++
		}
	}
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		// If the file is JSON, pass through; otherwise wrap as description.
		var probe map[string]any
		if json.Unmarshal(raw, &probe) == nil {
			return probe, nil
		}
		return map[string]any{"description": string(raw), "severity": severity, "source": "cli"}, nil
	}
	if text == "" {
		return nil, errors.New("--file or --text required")
	}
	return map[string]any{"description": text, "severity": severity, "source": "cli"}, nil
}

func corpusCmd(gateway string, args []string) error {
	if len(args) == 0 || args[0] != "ingest" {
		return errors.New("corpus ingest --source SRC --title T (--file PATH | --text \"...\")")
	}
	args = args[1:]
	var source, title, urlArg, file, text string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--source":
			source = args[i+1]; i++
		case "--title":
			title = args[i+1]; i++
		case "--url":
			urlArg = args[i+1]; i++
		case "--file":
			file = args[i+1]; i++
		case "--text":
			text = args[i+1]; i++
		}
	}
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		text = string(raw)
	}
	if title == "" || text == "" {
		return errors.New("--title and (--file or --text) are required")
	}
	body := map[string]any{"source": source, "title": title, "url": urlArg, "text": text}
	return doJSON(gateway+"/v1/corpus", body, os.Stdout)
}

func configCmd(gateway string, args []string) error {
	if len(args) == 0 || args[0] != "llm" {
		return errors.New("config llm --provider P [--model M] [--base-url U] [--key K]")
	}
	args = args[1:]
	var provider, model, baseURL, key string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			provider = args[i+1]; i++
		case "--model":
			model = args[i+1]; i++
		case "--base-url":
			baseURL = args[i+1]; i++
		case "--key":
			key = args[i+1]; i++
		}
	}
	body := map[string]any{"provider": provider, "model": model, "base_url": baseURL, "api_key": key}
	return doJSON(gateway+"/v1/config/llm", body, os.Stdout)
}

func doJSON(endpoint string, body any, out io.Writer) error {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(out, resp.Body) //nolint:errcheck
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	fmt.Fprintln(out)
	return nil
}

// watchSSE follows the SSE stream and pretty-prints events as they arrive.
func watchSSE(endpoint string) error {
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "text/event-stream")
	hc := &http.Client{Timeout: 0}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("SSE HTTP %d", resp.StatusCode)
	}
	br := bufio.NewReader(resp.Body)
	var lastEvent string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			lastEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			fmt.Printf("[%s] %s\n", lastEvent, strings.TrimPrefix(line, "data: "))
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func extractFlag(args []string, name, def string) ([]string, string) {
	out := make([]string, 0, len(args))
	v := def
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) {
			v = args[i+1]
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out, v
}
