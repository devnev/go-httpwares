// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package http_opentracing

import (
	"fmt"
	"net/http"

	"log"

	"github.com/mwitkow/go-httpwares"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
)

// UnaryClientInterceptor returns a new unary server interceptor for OpenTracing.
func Tripperware(opts ...Option) httpwares.Tripperware {
	o := evaluateOptions(opts)
	return func(next http.RoundTripper) http.RoundTripper {
		return httpwares.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if o.filterOutFunc != nil && !o.filterOutFunc(req) {
				return next.RoundTrip(req)
			}
			newReq, clientSpan := newClientSpanFromRequest(req, o.tracer)
			resp, err := next.RoundTrip(newReq)
			// record the trace.
			ext.HTTPStatusCode.Set(clientSpan, uint16(resp.StatusCode))
			if err != nil {
				ext.Error.Set(clientSpan, true)
				clientSpan.LogFields(otlog.String("event", "error"), otlog.String("message", err.Error()))
			}
			if o.statusCodeErrorFunc(resp.StatusCode) {
				ext.Error.Set(clientSpan, true)
			}
			clientSpan.Finish()
			return resp, err
		})
	}
}

func newClientSpanFromRequest(req *http.Request, tracer opentracing.Tracer) (*http.Request, opentracing.Span) {
	var parentSpanContext opentracing.SpanContext
	if parent := opentracing.SpanFromContext(req.Context()); parent != nil {
		parentSpanContext = parent.Context()
	}
	clientSpan := tracer.StartSpan(
		operationNameFromUrl(req),
		opentracing.ChildOf(parentSpanContext),
		ext.SpanKindRPCClient,
		httpTag,
	)
	ext.HTTPUrl.Set(clientSpan, req.URL.String())
	ext.HTTPMethod.Set(clientSpan, req.Method)

	// This makes a copy of the request, so that both headers and context are not affected.
	newReq := req.WithContext(opentracing.ContextWithSpan(req.Context(), clientSpan))
	if err := tracer.Inject(clientSpan.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(newReq.Header)); err != nil {
		log.Printf("http_opentracing: failed serializing trace information: %v", err)
	}
	return newReq, clientSpan
}

func operationNameFromUrl(req *http.Request) string {
	// String() doesn't work as it would serialize queries etc.
	return fmt.Sprintf("%s%s", req.URL.Host, req.URL.Path)
}
