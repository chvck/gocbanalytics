package httpqueryclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/google/uuid"

	"github.com/couchbaselabs/gocbanalytics/internal/leakcheck"
)

// Query executes a query.
func (c *Client) Query(ctx context.Context, opts *QueryOptions) (*QueryRowReader, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	statement := getMapValueString(opts.Payload, "statement", "")

	body, err := json.Marshal(opts.Payload)
	if err != nil {
		return nil, newObfuscateErrorWrapper("failed to marshal query payload", err)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	if opts.Priority != nil {
		header.Set("Analytics-Priority", fmt.Sprintf("%d", *opts.Priority))
	}

	ctxDeadline, _ := ctx.Deadline()

	var serverDeadline time.Time

	st, ok := opts.Payload["timeout"]
	if ok {
		timeout, err := time.ParseDuration(st.(string))
		if err != nil {
			return nil, newObfuscateErrorWrapper("failed to parse server timeout", err)
		}

		serverDeadline = time.Now().Add(timeout)
	}

	var lastCode uint32

	var lastMessage string

	var lastRootErr error

	var retries uint32

	uniqueID := uuid.NewString()

	backoff := columnarExponentialBackoffWithJitter(100*time.Millisecond, 1*time.Minute, 2)

	addrs, err := c.resolver.LookupHost(ctx, c.host)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup host: %w", err)
	}

	for {
		if len(addrs) == 0 {
			return nil, newColumnarError(lastRootErr, statement, c.host, 0).withLastDetail(lastCode, lastMessage)
		}

		idx := rand.Intn(len(addrs))
		addr := addrs[idx]

		reqURI := fmt.Sprintf("%s://%s:%d/api/v1/request", c.scheme, addr, c.port)

		var connectDoneErr error

		trace := &httptrace.ClientTrace{ //nolint:exhaustruct
			ConnectDone: func(_, _ string, err error) {
				connectDoneErr = err
			},
		}

		req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "POST", reqURI, io.NopCloser(bytes.NewReader(body)))
		if err != nil {
			return nil, newObfuscateErrorWrapper("failed to create http request", err)
		}

		req.Host = c.host
		req.Header = header

		username, password := opts.CredentialProvider()
		req.SetBasicAuth(username, password)

		c.logger.Trace("Sending request %s to %s", uniqueID, reqURI)

		resp, err := c.innerClient.Do(req)
		if err != nil {
			c.logger.Trace("Received HTTP Response for ID=%s, errored: %v", uniqueID, err)

			// We don't want to bail out on connection errors as they may be because of dial timeout.
			if connectDoneErr == nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil, newColumnarError(err, statement, c.host, 0)
				}
			}

			newBody, err := handleMaybeRetryColumnar(ctxDeadline, serverDeadline, backoff, retries, opts.Payload)
			if err != nil {
				return nil, newColumnarError(err, statement, c.host, 0).withLastDetail(lastCode, lastMessage)
			}

			addrs = append(addrs[:idx], addrs[idx+1:]...)
			lastRootErr = newObfuscateErrorWrapper("failed to send request", err)

			body = newBody
			retries++

			continue
		}

		c.logger.Trace("Received HTTP Response for ID=%s, status=%d", uniqueID, resp.StatusCode)

		resp = leakcheck.WrapHTTPResponse(resp) // nolint: bodyclose
		if resp.StatusCode != 200 {
			respBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return nil, newColumnarError(newObfuscateErrorWrapper("failed to read response body", readErr), statement,
					c.host, resp.StatusCode)
			}

			cErr := parseColumnarErrorResponse(respBody, statement, c.host, resp.StatusCode, lastCode, lastMessage)
			if cErr != nil {
				first, retriable := isColumnarErrorRetriable(cErr)
				if !retriable {
					return nil, cErr
				}

				lastRootErr = cErr

				if first != nil {
					lastCode = first.Code
					lastMessage = first.Message
				}

				newBody, err := handleMaybeRetryColumnar(ctxDeadline, serverDeadline, backoff, retries, opts.Payload)
				if err != nil {
					return nil, newColumnarError(err, statement, c.host, resp.StatusCode).
						withErrors(cErr.Errors).
						withErrorText(string(respBody)).
						withLastDetail(lastCode, lastMessage)
				}

				body = newBody
				retries++

				continue
			}

			return nil, newColumnarError(
				errors.New("query returned non-200 status code but no errors in body"), //nolint:err113
				statement,
				c.host,
				resp.StatusCode).
				withErrorText(string(respBody)).
				withLastDetail(lastCode, lastMessage)
		}

		streamer, err := newQueryStreamer(resp.Body, c.logger, "results")
		if err != nil {
			respBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				c.logger.Debug("Failed to read response body: %v", readErr)
			}

			return nil, newColumnarError(newObfuscateErrorWrapper("failed to parse success response body", readErr),
				statement,
				c.host,
				resp.StatusCode).
				withErrorText(string(respBody)).
				withLastDetail(lastCode, lastMessage)
		}

		peeked := streamer.NextRow()
		if peeked == nil {
			err := streamer.Err()
			if err != nil {
				return nil, newColumnarError(err,
					statement,
					c.host,
					resp.StatusCode)
			}

			meta, metaErr := streamer.MetaData()
			if metaErr != nil {
				return nil, newColumnarError(metaErr,
					statement,
					c.host,
					resp.StatusCode)
			}

			cErr := parseColumnarErrorResponse(meta, statement, c.host, resp.StatusCode, lastCode, lastMessage)
			if cErr != nil {
				first, retriable := isColumnarErrorRetriable(cErr)
				if !retriable {
					return nil, cErr
				}

				lastRootErr = cErr

				if first != nil {
					lastCode = first.Code
					lastMessage = first.Message
				}

				newBody, err := handleMaybeRetryColumnar(ctxDeadline, serverDeadline, backoff, retries, opts.Payload)
				if err != nil {
					return nil, newColumnarError(err, statement, c.host, resp.StatusCode).
						withErrors(cErr.Errors).
						withErrorText(string(meta)).
						withLastDetail(lastCode, lastMessage)
				}

				body = newBody
				retries++

				continue
			}
		}

		return &QueryRowReader{
			streamer:   streamer,
			statement:  statement,
			endpoint:   c.host,
			statusCode: resp.StatusCode,
			peeked:     peeked,
		}, nil
	}
}

func parseColumnarErrorResponse(respBody []byte, statement, endpoint string, statusCode int, lastCode uint32, lastMsg string) *QueryError {
	var rawRespParse jsonAnalyticsErrorResponse

	if statusCode == 401 {
		return newColumnarError(ErrInvalidCredential, statement, endpoint, statusCode)
	}

	parseErr := json.Unmarshal(respBody, &rawRespParse)
	if parseErr != nil {
		return newColumnarError(newObfuscateErrorWrapper("failed to parse response errors", parseErr), statement, endpoint, statusCode).
			withLastDetail(lastCode, lastMsg).
			withErrorText(string(respBody))
	}

	if len(rawRespParse.Errors) == 0 {
		return nil
	}

	var respParse []jsonAnalyticsError

	parseErr = json.Unmarshal(rawRespParse.Errors, &respParse)
	if parseErr != nil {
		return newColumnarError(newObfuscateErrorWrapper("failed to parse response errors", parseErr), statement, endpoint, statusCode).
			withLastDetail(lastCode, lastMsg).
			withErrorText(string(respBody))
	}

	if len(respParse) == 0 {
		return nil
	}

	var innerErr error

	errDescs := make([]ErrorDesc, len(respParse))
	for i, jsonErr := range respParse {
		errDescs[i] = ErrorDesc{
			Code:    jsonErr.Code,
			Message: jsonErr.Msg,
			Retry:   jsonErr.Retry,
		}

		if innerErr == nil {
			if jsonErr.Code == 21002 {
				innerErr = ErrTimeout
			} else if jsonErr.Code == 20000 {
				innerErr = ErrInvalidCredential
			}
		}
	}

	if innerErr == nil {
		innerErr = ErrColumnar
	}

	return newColumnarError(innerErr, statement, endpoint, statusCode).
		withLastDetail(lastCode, lastMsg).
		withErrorText(string(respBody)).
		withErrors(errDescs)
}

func isColumnarErrorRetriable(cErr *QueryError) (*ErrorDesc, bool) {
	// If there are no errors then we shouldn't retry.
	if len(cErr.Errors) == 0 {
		return nil, false
	}

	var first *ErrorDesc

	allRetriable := true

	for _, err := range cErr.Errors {
		if !err.Retry {
			allRetriable = false

			if first == nil {
				first = &ErrorDesc{
					Code:    err.Code,
					Message: err.Message,
					Retry:   false,
				}
			}
		}
	}

	if !allRetriable {
		return nil, false
	}

	if first == nil && len(cErr.Errors) > 0 {
		first = &cErr.Errors[0]
	}

	return first, true
}

// Note in the interest of keeping this signature sane, we return a raw base error here.
func handleMaybeRetryColumnar(ctxDeadline time.Time, serverDeadline time.Time, calc backoffCalculator,
	retries uint32, payload map[string]interface{}) ([]byte, error) {
	b := calc(retries)

	var body []byte

	if !ctxDeadline.IsZero() {
		if time.Now().Add(b).After(ctxDeadline.Add(-b)) {
			return nil, ErrContextDeadlineWouldBeExceeded
		}
	}

	if !serverDeadline.IsZero() {
		serverTimeout := serverDeadline.Sub(time.Now().Add(b))

		if serverTimeout < 0 {
			return nil, ErrTimeout
		}

		payload["timeout"] = serverTimeout.String()

		payloadBody, err := json.Marshal(payload)
		if err != nil {
			return nil, newObfuscateErrorWrapper("failed to marshal payload after updating timeout", err)
		}

		body = payloadBody
	}

	time.Sleep(b)

	return body, nil
}

type backoffCalculator func(retryAttempts uint32) time.Duration

func columnarExponentialBackoffWithJitter(min, max time.Duration, backoffFactor float64) backoffCalculator { //nolint:revive
	var minBackoff float64 = 1000000 // 1 Millisecond

	var maxBackoff float64 = 500000000 // 500 Milliseconds

	var factor float64 = 2

	if min > 0 {
		minBackoff = float64(min)
	}

	if max > 0 {
		maxBackoff = float64(max)
	}

	if backoffFactor > 0 {
		factor = backoffFactor
	}

	return func(retryAttempts uint32) time.Duration {
		backoff := minBackoff * (math.Pow(factor, float64(retryAttempts)))

		backoff = rand.Float64() * (backoff) // #nosec G404

		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		if backoff < minBackoff {
			backoff = minBackoff
		}

		return time.Duration(backoff)
	}
}
