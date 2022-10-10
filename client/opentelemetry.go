package client

import (
	"net/http"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel/trace"
)

type openTelemetryTransport struct {
	transport runtime.ClientTransport
	host      string
	//TODO: Find the otel equivalent of span options
	opts []trace.Span
}

func newOpenTelemetryTransport(transport runtime.ClientTransport, host string, opts []trace.Span,
) runtime.ClientTransport {
	return &openTelemetryTransport{
		transport: transport,
		host:      host,
		opts:      opts,
	}
}

func (t *openTelemetryTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	if op.Context == nil {
		return t.transport.Submit(op)
	}

	params := op.Params
	reader := op.Reader

	var span trace.Span
	defer func() {
		if span != nil {
			span.End()
		}
	}()

	op.Params = runtime.ClientRequestWriterFunc(func(req runtime.ClientRequest, reg strfmt.Registry) error {
		span = createClientSpan(op, req.GetHeaderParams(), t.host, t.opts)
		return params.WriteToRequest(req, reg)
	})

	op.Reader = runtime.ClientResponseReaderFunc(func(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
		if span != nil {
			code := response.Code()
			ext.HTTPStatusCode.Set(span, uint16(code))
			if code >= 400 {
				ext.Error.Set(span, true)
			}
		}
		return reader.ReadResponse(response, consumer)
	})

	submit, err := t.transport.Submit(op)
	if err != nil && span != nil {
		ext.Error.Set(span, true)
		span.LogFields(log.Error(err))
	}
	return submit, err
}

func createOpenTelemetryClientSpan(op *runtime.ClientOperation, header http.Header, host string,
	opts []opentracing.StartSpanOption) trace.Span {
	ctx := op.Context
	span := trace.SpanFromContext(ctx)

	if span != nil {
		opts = append(opts, ext.SpanKindRPCClient)
		span, _ = opentracing.StartSpanFromContextWithTracer(
			ctx, span.Tracer(), operationName(op), opts...)

		ext.Component.Set(span, "go-openapi")
		ext.PeerHostname.Set(span, host)
		span.SetTag("http.path", op.PathPattern)
		ext.HTTPMethod.Set(span, op.Method)

		_ = span.Tracer().Inject(
			span.Context(),
			opentracing.HTTPHeaders,
			opentracing.HTTPHeadersCarrier(header))

		return span
	}
	return nil
}
