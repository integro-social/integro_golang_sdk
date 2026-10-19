package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	host    string
	http    *http.Client
	mu      sync.Mutex
	headers http.Header
}

func NewClient(host string) *Client {
	return &Client{host: strings.TrimRight(host, "/"), http: &http.Client{}, headers: http.Header{}}
}

func (c *Client) SetHeader(key, value string) {
	c.mu.Lock()
	c.headers.Set(key, value)
	c.mu.Unlock()
}

func (c *Client) SetBearer(token string) {
	if token == "" {
		c.mu.Lock()
		c.headers.Del("Authorization")
		c.mu.Unlock()
		return
	}
	c.SetHeader("Authorization", "Bearer "+token)
}

func (c *Client) headerClone() http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.headers.Clone()
}

type ApiError struct {
	Kind    string
	Code    int
	Message string
	Body    string
}

func (e *ApiError) Error() string {
	switch e.Kind {
	case "api":
		return fmt.Sprintf("api error %d: %s", e.Code, e.Message)
	default:
		return fmt.Sprintf("%s error: %s", e.Kind, e.Message)
	}
}

func encodePath(v string) string { return url.PathEscape(v) }

func (c *Client) buildURL(path string, query any) (string, error) {
	u := c.host + path
	if query == nil {
		return u, nil
	}
	raw, err := json.Marshal(query)
	if err != nil {
		return "", &ApiError{Kind: "parse", Message: err.Error()}
	}
	var m any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", &ApiError{Kind: "parse", Message: err.Error()}
	}
	var segs []string
	appendQuery(&segs, "", m)
	if len(segs) > 0 {
		u += "?" + strings.Join(segs, "&")
	}
	return u, nil
}

func appendQuery(out *[]string, key string, value any) {
	switch v := value.(type) {
	case nil:
		if key != "" {
			*out = append(*out, url.QueryEscape(key))
		}
	case map[string]any:
		for k, vv := range v {
			child := k
			if key != "" {
				child = key + "[" + k + "]"
			}
			appendQuery(out, child, vv)
		}
	case []any:
		for i, vv := range v {
			appendQuery(out, key+"["+strconv.Itoa(i)+"]", vv)
		}
	case bool:
		*out = append(*out, url.QueryEscape(key)+"="+strconv.FormatBool(v))
	case float64:
		*out = append(*out, url.QueryEscape(key)+"="+strconv.FormatFloat(v, 'f', -1, 64))
	case string:
		*out = append(*out, url.QueryEscape(key)+"="+url.QueryEscape(v))
	default:
		*out = append(*out, url.QueryEscape(key)+"="+url.QueryEscape(fmt.Sprintf("%v", v)))
	}
}

func (c *Client) do(method, path string, query any, body any) (*http.Response, error) {
	u, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, &ApiError{Kind: "parse", Message: err.Error()}
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return nil, &ApiError{Kind: "network", Message: err.Error()}
	}
	req.Header = c.headerClone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &ApiError{Kind: "network", Message: err.Error()}
	}
	return resp, nil
}

func request[R any](c *Client, method, path string, query any, body any) (R, error) {
	var zero R
	resp, err := c.do(method, path, query, body)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, &ApiError{Kind: "api", Code: resp.StatusCode, Message: resp.Status, Body: string(raw)}
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, &ApiError{Kind: "parse", Message: err.Error()}
	}
	return zero, nil
}

func (c *Client) requestBytes(method, path string, query any, body any) ([]byte, error) {
	resp, err := c.do(method, path, query, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ApiError{Kind: "api", Code: resp.StatusCode, Message: resp.Status, Body: string(raw)}
	}
	return raw, nil
}

type MultipartForm struct {
	buf         bytes.Buffer
	writer      *multipart.Writer
	initialized bool
}

func NewMultipartForm() *MultipartForm {
	f := &MultipartForm{}
	f.writer = multipart.NewWriter(&f.buf)
	f.initialized = true
	return f
}

func (f *MultipartForm) Field(name, value string) { _ = f.writer.WriteField(name, value) }

func (f *MultipartForm) File(name, filename string, content []byte) {
	w, err := f.writer.CreateFormFile(name, filename)
	if err == nil {
		_, _ = w.Write(content)
	}
}

func requestMultipart[R any](c *Client, method, path string, query any, form *MultipartForm) (R, error) {
	var zero R
	_ = form.writer.Close()
	u, err := c.buildURL(path, query)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequest(method, u, bytes.NewReader(form.buf.Bytes()))
	if err != nil {
		return zero, &ApiError{Kind: "network", Message: err.Error()}
	}
	req.Header = c.headerClone()
	req.Header.Set("Content-Type", form.writer.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return zero, &ApiError{Kind: "network", Message: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, &ApiError{Kind: "api", Code: resp.StatusCode, Message: resp.Status, Body: string(raw)}
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, &ApiError{Kind: "parse", Message: err.Error()}
	}
	return zero, nil
}

func (c *Client) sse(path string, query any, onEvent func(json.RawMessage)) error {
	u, err := c.buildURL(path, query)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return &ApiError{Kind: "network", Message: err.Error()}
	}
	req.Header = c.headerClone()
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return &ApiError{Kind: "network", Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ApiError{Kind: "api", Code: resp.StatusCode, Message: resp.Status}
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				onEvent(json.RawMessage(strings.Join(data, "\n")))
				data = data[:0]
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(rest, " "))
		}
	}
	if len(data) > 0 {
		onEvent(json.RawMessage(strings.Join(data, "\n")))
	}
	return nil
}

type WsStatus int

const (
	WsConnecting WsStatus = iota
	WsOpen
	WsReconnecting
	WsStopped
)

type WsHandlers[R any] struct {
	OnMessage func(R)
	OnBinary  func([]byte)
	OnOpen    func()
	OnClose   func()
	OnError   func(error)
	OnStatus  func(WsStatus)
}

type wsFrame struct {
	kind int
	data []byte
}

type WsConnection[S any] struct {
	send   chan wsFrame
	stop   chan struct{}
	closed sync.Once
}

func (w *WsConnection[S]) Send(message S) bool {
	raw, err := json.Marshal(message)
	if err != nil {
		return false
	}
	return w.enqueue(wsFrame{kind: websocket.TextMessage, data: raw})
}

func (w *WsConnection[S]) SendBinary(data []byte) bool {
	return w.enqueue(wsFrame{kind: websocket.BinaryMessage, data: data})
}

func (w *WsConnection[S]) enqueue(frame wsFrame) bool {
	select {
	case w.send <- frame:
		return true
	case <-w.stop:
		return false
	}
}

func (w *WsConnection[S]) Stop() {
	w.closed.Do(func() { close(w.stop) })
}

func buildWsURL(c *Client, path string, query any) (string, error) {
	u, err := c.buildURL(path, query)
	if err != nil {
		return "", err
	}
	if rest, ok := strings.CutPrefix(u, "https://"); ok {
		return "wss://" + rest, nil
	}
	if rest, ok := strings.CutPrefix(u, "http://"); ok {
		return "ws://" + rest, nil
	}
	return u, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Request[R any](c *Client, method, path string, query any, body any) (R, error) {
	return request[R](c, method, path, query, body)
}

func RequestBytes(c *Client, method, path string, query any, body any) ([]byte, error) {
	return c.requestBytes(method, path, query, body)
}

func RequestMultipart[R any](c *Client, method, path string, query any, form *MultipartForm) (R, error) {
	return requestMultipart[R](c, method, path, query, form)
}

func Sse(c *Client, path string, query any, onEvent func(json.RawMessage)) error {
	return c.sse(path, query, onEvent)
}

func Ws[S, R any](c *Client, path string, query any, handlers WsHandlers[R]) *WsConnection[S] {
	conn := &WsConnection[S]{send: make(chan wsFrame, 16), stop: make(chan struct{})}
	status := func(s WsStatus) {
		if handlers.OnStatus != nil {
			handlers.OnStatus(s)
		}
	}
	fail := func(err error) {
		if handlers.OnError != nil {
			handlers.OnError(err)
		}
	}
	wsURL, err := buildWsURL(c, path, query)
	if err != nil {
		fail(err)
		conn.Stop()
		status(WsStopped)
		return conn
	}
	go func() {
		failures := 0
		for {
			select {
			case <-conn.stop:
				status(WsStopped)
				return
			default:
			}
			if failures == 0 {
				status(WsConnecting)
			} else {
				status(WsReconnecting)
			}
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, c.headerClone())
			if err != nil {
				fail(err)
			} else {
				failures = 0
				status(WsOpen)
				if handlers.OnOpen != nil {
					handlers.OnOpen()
				}
				done := make(chan struct{})
				go func() {
					for {
						kind, msg, err := ws.ReadMessage()
						if err != nil {
							close(done)
							return
						}
						switch kind {
						case websocket.TextMessage:
							if handlers.OnMessage != nil {
								var decoded R
								if err := json.Unmarshal(msg, &decoded); err != nil {
									fail(&ApiError{Kind: "parse", Message: err.Error()})
									continue
								}
								handlers.OnMessage(decoded)
							}
						case websocket.BinaryMessage:
							if handlers.OnBinary != nil {
								handlers.OnBinary(msg)
							}
						}
					}
				}()
			writeLoop:
				for {
					select {
					case <-conn.stop:
						_ = ws.Close()
						status(WsStopped)
						return
					case <-done:
						break writeLoop
					case out := <-conn.send:
						if err := ws.WriteMessage(out.kind, out.data); err != nil {
							break writeLoop
						}
					}
				}
				_ = ws.Close()
				if handlers.OnClose != nil {
					handlers.OnClose()
				}
			}
			select {
			case <-conn.stop:
				status(WsStopped)
				return
			default:
			}
		drain:
			for {
				select {
				case <-conn.send:
				default:
					break drain
				}
			}
			backoff := 500 * (1 << min(failures, 5))
			if backoff > 15000 {
				backoff = 15000
			}
			failures++
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(backoff)*time.Millisecond)
			select {
			case <-conn.stop:
				cancel()
				status(WsStopped)
				return
			case <-ctx.Done():
				cancel()
			}
		}
	}()
	return conn
}

func EncodePath(v string) string { return encodePath(v) }
