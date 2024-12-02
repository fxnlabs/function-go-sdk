package function_go_sdk

import (
	"buf.build/gen/go/fxnlabs/api-gateway/connectrpc/go/apigateway/v1/apigatewayv1connect"
	apigatewayv1 "buf.build/gen/go/fxnlabs/api-gateway/protocolbuffers/go/apigateway/v1"
	"connectrpc.com/connect"
	"context"
	"errors"
	"io"
	"net/http"
)

// DefaultBaseUrl is the default Function Network API gateway base URL.
const DefaultBaseUrl = "https://api.function.network"

// MissingApiKeyError is returned when a client was being created, but no API key was provided for it to use.
var MissingApiKeyError = errors.New("missing API key")

// TruncatedStreamResponseError is returned when a streaming response was truncated and required information could not be obtained from it.
// This can be indicative of a network issue or an API gateway malfunction.
var TruncatedStreamResponseError = errors.New("the stream response was truncated")

// ResponseStream is a streaming response that can be read chunk-by-chunk.
// Calling Read on one will return a chunk or an error.
// If the stream is complete, the error will be io.EOF.
// Any subsequent calls to a complete stream will yield io.EOF.
//
// If you are no longer interested in a stream, you may call Close.
// Once Close is called, the server will be notified to stop sending chunks, and subsequent calls to Read will yield io.EOF.
type ResponseStream[TIn any, TOut any] struct {
	isClosed    bool
	stream      *connect.ServerStreamForClient[TIn]
	transformer func(*TIn) TOut
}

// IsClosed returns whether the stream is closed, either forcibly by the client or server, or naturally due to the stream ending.
// If Read ever returned io.EOF, this will return true.
// Note that this method cannot tell whether the connection was closed since the last call to Read.
func (r *ResponseStream[TIn, TOut]) IsClosed() bool {
	return r.isClosed
}

// Read receives a single chunk from the stream.
// If the stream is done, or has been closed, the error will be io.EOF.
// If there was an error returned or a connection issue, a non-EOF error will be returned.
func (r *ResponseStream[TIn, TOut]) Read() (TOut, error) {
	var empty TOut

	if r.isClosed {
		return empty, io.EOF
	}

	if !r.stream.Receive() {
		r.isClosed = true
		return empty, io.EOF
	}

	return r.transformer(r.stream.Msg()), r.stream.Err()
}

// Close ends the stream.
// Any subsequent calls to Read will yield io.EOF.
// Even if an error is returned, the stream will still be considered closed.
func (r *ResponseStream[TIn, TOut]) Close() error {
	// Regardless of whether the connection shutdown succeeded or not,
	// we still want to prevent any further reads.
	r.isClosed = true
	return r.stream.Close()
}

// Creates a new ResponseStream that wraps *connect.ServerStreamForClient.
func wrapStream[TIn any, TOut any](stream *connect.ServerStreamForClient[TIn], transformer func(*TIn) TOut) *ResponseStream[TIn, TOut] {
	return &ResponseStream[TIn, TOut]{
		isClosed:    false,
		stream:      stream,
		transformer: transformer,
	}
}

// ChatCompleteStreamResponse is a streaming response for ChatCompleteStream.
// The response includes the role of the response message, and a readable stream of output tokens.
type ChatCompleteStreamResponse struct {
	// Role is the role for the response message.
	Role string

	// TokenStream is the stream of response tokens.
	TokenStream *ResponseStream[apigatewayv1.ChatCompleteStreamResponse, string]
}

// Transformer used for ChatCompleteStreamResponse.
func chatCompleteStreamToStringTransformer(res *apigatewayv1.ChatCompleteStreamResponse) string {
	return res.Response.Content
}

// HttpClient is an interface that defines an HTTP client.
// It has one method, Do, which takes in *http.Request and returns (*http.Response, error).
// The default Go http.Client implements this interface.
type HttpClient interface {
	// HTTPClient is wrapped to avoid users needing to directly depend on connect.HTTPClient.
	connect.HTTPClient
}

// ClientOptions are options used to configure a Function Network client.
type ClientOptions struct {
	// ApiKey is the API key used to authenticate calls made to the network.
	// Required.
	ApiKey string

	// HttpClient is the HTTP client to use for making calls to Function.
	// If unspecified, the default Go HTTP client (http.DefaultClient) will be used.
	HttpClient HttpClient

	// The Function Network API gateway base URL.
	// If unspecified, defaults to DefaultBaseUrl.
	// Most users will not need to specify a value here.
	BaseUrl string
}

// Client is a client that can interact with the Function Network.
// Clients contain authentication information and can make inference calls.
type Client struct {
	// The API key used for authenticating requests.
	apiKey string

	// The underlying gRPC service that will be interacted with.
	service apigatewayv1connect.APIGatewayServiceClient
}

func newAuthInterceptor(apiKey string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(
			ctx context.Context,
			req connect.AnyRequest,
		) (connect.AnyResponse, error) {
			req.Header().Set("x-api-key", apiKey)

			return next(ctx, req)
		}
	}
}

// NewClient creates a new Function Network client using the provided options.
// If no API key is specified in the client options, MissingApiKeyError will be returned.
// If the client was successfully created, the newly created Client will be returned along with a nil error.
//
// Note that simply creating a client will not create any connections or perform any requests.
func NewClient(options ClientOptions) (*Client, error) {
	if options.ApiKey == "" {
		return nil, MissingApiKeyError
	}

	var httpClient HttpClient
	if options.HttpClient == nil {
		httpClient = http.DefaultClient
	} else {
		httpClient = options.HttpClient
	}

	var baseUrl string
	if options.BaseUrl == "" {
		baseUrl = DefaultBaseUrl
	} else {
		baseUrl = options.BaseUrl
	}

	service := apigatewayv1connect.NewAPIGatewayServiceClient(
		httpClient,
		baseUrl,
		connect.WithInterceptors(newAuthInterceptor(options.ApiKey)),
	)

	return &Client{
		apiKey:  options.ApiKey,
		service: service,
	}, nil
}

// ChatComplete takes in a list of messages, each with a role and content, and generates the next reply in the chain.
// The entire response is returned at once in a blocking fashion with this function.
// The response token count is returned with the response.
// If you would like to stream each token as it is generated, use ChatCompleteStream instead.
//
// Please refer to the developer docs to find a suitable model to use.
func (c *Client) ChatComplete(ctx context.Context, request *apigatewayv1.ChatCompleteRequest) (*apigatewayv1.ChatCompleteResponse, error) {
	res, err := c.service.ChatComplete(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}

	return res.Msg, nil
}

// ChatCompleteStream takes in a list of messages, each with a role and content, and generates the next reply in the chain.
// Each token is streamed one-by-one, and the response can be canceled by closing the stream.
// If you would like to receive the entire response at once in a blocking fashion, use ChatComplete instead.
//
// Please refer to the developer docs to find a suitable model to use.
func (c *Client) ChatCompleteStream(ctx context.Context, request *apigatewayv1.ChatCompleteStreamRequest) (*ChatCompleteStreamResponse, error) {
	res, err := c.service.ChatCompleteStream(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}

	// Read the first chunk to get the role.
	if !res.Receive() {
		return nil, TruncatedStreamResponseError
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	firstMsg := res.Msg()

	return &ChatCompleteStreamResponse{
		Role:        firstMsg.Response.Role,
		TokenStream: wrapStream(res, chatCompleteStreamToStringTransformer),
	}, nil
}

// Embed takes in input string(s) and returns the generated vector embeddings.
//
// Please refer to the developer docs to find a suitable model to use.
func (c *Client) Embed(ctx context.Context, request *apigatewayv1.EmbedRequest) (*apigatewayv1.EmbedResponse, error) {
	res, err := c.service.Embed(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}

	return res.Msg, nil
}

// TextToImage takes in a text prompt and some parameters and generates an image based on the input prompt.
// The image is returned as a downloadable URL.
// Returned image URLs are not guaranteed to be available indefinitely, so they should not be treated as long-term CDN URLs.
//
// Please refer to the developer docs to find a suitable model to use.
func (c *Client) TextToImage(ctx context.Context, request *apigatewayv1.TextToImageRequest) (*apigatewayv1.TextToImageResponse, error) {
	res, err := c.service.TextToImage(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}

	return res.Msg, nil
}

// Transcribe takes in a URL to some audio and transcribes speech within it.
// The transcribed text is returned as a block of text, as well as in a list with timestamps at which individual words occur.
// Audio format support varies by model, but common formats such as WAV and MP3 are generally supported.
//
// Please refer to the developer docs to find a suitable model to use.
func (c *Client) Transcribe(ctx context.Context, request *apigatewayv1.TranscribeRequest) (*apigatewayv1.TranscribeResponse, error) {
	res, err := c.service.Transcribe(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, err
	}

	return res.Msg, nil
}
