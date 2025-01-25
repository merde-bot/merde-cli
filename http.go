// Copyright 2025 Bold Software, Inc. (https://merde.ai/)
// Released under the PolyForm Noncommercial License 1.0.0.
// Please see the README for details.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/carlmjohnson/requests"
)

var (
	// global client api versioning
	// probably never use them, shrug
	apiRequestVersion  = "1"
	apiResponseVersion = "1"
)

func baseRequest(cfg *Config) *requests.Builder {
	return requests.New().
		Bearer(cfg.Get(tokenKey)).
		Accept("multipart/mixed").
		Header("Git-Version", cfg.GitVersion).
		Header("Merde-Client-Version", version).
		Header("Merde-Client-Commit", commit).
		Header("Merde-Client-Date", date).
		Header("Merde-Client-OS", runtime.GOOS).
		Header("Merde-Client-Arch", runtime.GOARCH).
		Header("Merde-Client-Go", runtime.Version()).
		Header("Merde-Client-API-Version", apiRequestVersion).
		BaseURL(cfg.Get(serverRootKey))
}

func rootRequest(ctx context.Context, cfg *Config) (*http.Request, error) {
	return baseRequest(cfg).Path("/cli/root").Method("GET").Request(ctx)
}

func checkAuthRequest(ctx context.Context, cfg *Config) (*http.Request, error) {
	return baseRequest(cfg).Path("/cli/check-auth").Method("GET").Request(ctx)
}

func helpRequest(ctx context.Context, cfg *Config, args []string) (*http.Request, error) {
	return baseRequest(cfg).Path("/cli/help").Param("args", args...).Method("GET").Request(ctx)
}

func deconflictRequest(ctx context.Context, cfg *Config, info *deconflictRequestInfo) (*http.Request, error) {
	remotes, _ := cfg.Git.Remotes(ctx) // best effort
	req := baseRequest(cfg).
		Path("/cli/"+info.verb+"/").
		Param("args", info.args...).
		Header("Main-Ref", info.mainRef).
		Header("Topic-Ref", info.topicRef).
		Header("Main-SHA", info.mainSHA).
		Header("Topic-SHA", info.topicSHA).
		Header("Pack-Size", fmt.Sprintf("%d", len(info.pack))).
		Method("POST").
		BodyReader(strings.NewReader(info.pack))
	for _, remote := range remotes {
		req = req.Header("Remote", remote)
	}
	return req.Request(ctx)
}

// A Response is a response from the server.
// It is a union type between a JSON response and a binary response.
type Response struct {
	IsJSON bool `json:"-"` // true if this is a JSON response, false if this is a binary response

	// JSON response fields
	Stdout   string `json:"stdout"`    // write this to stdout
	Stderr   string `json:"stderr"`    // write this to stderr
	ExitCode int    `json:"exit_code"` // if > 0, the client should exit with this exit code

	// Git response fields
	// If both non-empty, create ref -> sha.
	Ref string `json:"ref"`
	SHA string `json:"sha"`

	// Binary response fields
	Data *bytes.Buffer `json:"-"`
}

// Process auto-handles json responses and reports whether it was processed.
func (r *Response) Process(ctx context.Context, cfg *Config) (bool, error) {
	if !r.IsJSON {
		return false, nil
	}
	if r.Ref != "" && r.SHA != "" {
		err := cfg.Git.CreateRef(ctx, r.Ref, r.SHA)
		if err != nil {
			return false, err
		}
	}
	if r.Stdout != "" {
		fmt.Print(r.Stdout)
	}
	if r.Stderr != "" {
		fmt.Fprint(os.Stderr, r.Stderr)
	}
	if r.ExitCode > 0 {
		os.Exit(r.ExitCode)
	}
	return true, nil
}

func doRequest(req *http.Request) iter.Seq2[*Response, error] {
	return func(yield func(*Response, error) bool) {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			yield(nil, err)
			return
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			// continued below
		default:
			buf, _ := io.ReadAll(resp.Body)
			err := fmt.Errorf("unexpected status code %d for %s: %s", resp.StatusCode, req.URL, string(buf))
			yield(nil, err)
			return
		}

		serverVersion := resp.Header.Get("Merde-Server-API-Version")
		if serverVersion != apiResponseVersion {
			err := fmt.Errorf("unexpected response version %q, please update this client", serverVersion)
			yield(nil, err)
			return
		}

		mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil {
			yield(nil, err)
			return
		}
		if !strings.HasPrefix(mediaType, "multipart/") {
			err := fmt.Errorf("unexpected content type: %s", mediaType)
			yield(nil, err)
			return
		}
		mr := multipart.NewReader(resp.Body, params["boundary"])

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}
			switch p.Header.Get("Content-Type") {
			case "application/json":
				var r Response
				err = json.NewDecoder(p).Decode(&r)
				if err != nil {
					yield(nil, err)
					return
				}
				r.IsJSON = true
				if !yield(&r, nil) {
					return
				}
			case "application/octet-stream":
				buf := new(bytes.Buffer)
				_, err := io.Copy(buf, p)
				if err != nil {
					yield(nil, err)
					return
				}
				r := &Response{Data: buf}
				if !yield(r, nil) {
					return
				}
			default:
				err := fmt.Errorf("unexpected multipart content type: %s", p.Header.Get("Content-Type"))
				yield(nil, err)
				return
			}
		}
	}
}
