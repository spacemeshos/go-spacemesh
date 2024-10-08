//go:build go1.22

// Package server provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.4.0 DO NOT EDIT.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/oapi-codegen/runtime"
	strictnethttp "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	externalRef0 "github.com/spacemeshos/go-spacemesh/api/node/models"
)

// Defines values for PostPublishProtocolParamsProtocol.
const (
	Ax1 PostPublishProtocolParamsProtocol = "ax1"
	B1  PostPublishProtocolParamsProtocol = "b1"
	Bc1 PostPublishProtocolParamsProtocol = "bc1"
	Bf1 PostPublishProtocolParamsProtocol = "bf1"
	Bo1 PostPublishProtocolParamsProtocol = "bo1"
	Bp1 PostPublishProtocolParamsProtocol = "bp1"
	Bw1 PostPublishProtocolParamsProtocol = "bw1"
	Mp1 PostPublishProtocolParamsProtocol = "mp1"
	Pp1 PostPublishProtocolParamsProtocol = "pp1"
	Tx1 PostPublishProtocolParamsProtocol = "tx1"
)

// PostPublishProtocolParamsProtocol defines parameters for PostPublishProtocol.
type PostPublishProtocolParamsProtocol string

// ServerInterface represents all server handlers.
type ServerInterface interface {
	// Get ATX by ID
	// (GET /activation/atx/{atx_id})
	GetActivationAtxAtxId(w http.ResponseWriter, r *http.Request, atxId externalRef0.ATXID)
	// Get last ATX by node ID
	// (GET /activation/last_atx/{node_id})
	GetActivationLastAtxNodeId(w http.ResponseWriter, r *http.Request, nodeId externalRef0.NodeID)
	// Get Positioning ATX ID with given maximum publish epoch
	// (GET /activation/positioning_atx/{publish_epoch})
	GetActivationPositioningAtxPublishEpoch(w http.ResponseWriter, r *http.Request, publishEpoch externalRef0.EpochID)
	// Publish a signed hare message
	// (POST /hare/publish)
	PostHarePublish(w http.ResponseWriter, r *http.Request)
	// Get a hare message to sign
	// (GET /hare/round_template/{layer}/{round})
	GetHareRoundTemplateLayerRound(w http.ResponseWriter, r *http.Request, layer externalRef0.LayerID, round externalRef0.HareRound)
	// Store PoET proof
	// (POST /poet)
	PostPoet(w http.ResponseWriter, r *http.Request)
	// Publish a blob in the given p2p protocol
	// (POST /publish/{protocol})
	PostPublishProtocol(w http.ResponseWriter, r *http.Request, protocol PostPublishProtocolParamsProtocol)
}

// ServerInterfaceWrapper converts contexts to parameters.
type ServerInterfaceWrapper struct {
	Handler            ServerInterface
	HandlerMiddlewares []MiddlewareFunc
	ErrorHandlerFunc   func(w http.ResponseWriter, r *http.Request, err error)
}

type MiddlewareFunc func(http.Handler) http.Handler

// GetActivationAtxAtxId operation middleware
func (siw *ServerInterfaceWrapper) GetActivationAtxAtxId(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "atx_id" -------------
	var atxId externalRef0.ATXID

	err = runtime.BindStyledParameterWithOptions("simple", "atx_id", r.PathValue("atx_id"), &atxId, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "atx_id", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetActivationAtxAtxId(w, r, atxId)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// GetActivationLastAtxNodeId operation middleware
func (siw *ServerInterfaceWrapper) GetActivationLastAtxNodeId(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "node_id" -------------
	var nodeId externalRef0.NodeID

	err = runtime.BindStyledParameterWithOptions("simple", "node_id", r.PathValue("node_id"), &nodeId, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "node_id", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetActivationLastAtxNodeId(w, r, nodeId)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// GetActivationPositioningAtxPublishEpoch operation middleware
func (siw *ServerInterfaceWrapper) GetActivationPositioningAtxPublishEpoch(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "publish_epoch" -------------
	var publishEpoch externalRef0.EpochID

	err = runtime.BindStyledParameterWithOptions("simple", "publish_epoch", r.PathValue("publish_epoch"), &publishEpoch, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "publish_epoch", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetActivationPositioningAtxPublishEpoch(w, r, publishEpoch)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// PostHarePublish operation middleware
func (siw *ServerInterfaceWrapper) PostHarePublish(w http.ResponseWriter, r *http.Request) {

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostHarePublish(w, r)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// GetHareRoundTemplateLayerRound operation middleware
func (siw *ServerInterfaceWrapper) GetHareRoundTemplateLayerRound(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "layer" -------------
	var layer externalRef0.LayerID

	err = runtime.BindStyledParameterWithOptions("simple", "layer", r.PathValue("layer"), &layer, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "layer", Err: err})
		return
	}

	// ------------- Path parameter "round" -------------
	var round externalRef0.HareRound

	err = runtime.BindStyledParameterWithOptions("simple", "round", r.PathValue("round"), &round, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "round", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.GetHareRoundTemplateLayerRound(w, r, layer, round)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// PostPoet operation middleware
func (siw *ServerInterfaceWrapper) PostPoet(w http.ResponseWriter, r *http.Request) {

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostPoet(w, r)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

// PostPublishProtocol operation middleware
func (siw *ServerInterfaceWrapper) PostPublishProtocol(w http.ResponseWriter, r *http.Request) {

	var err error

	// ------------- Path parameter "protocol" -------------
	var protocol PostPublishProtocolParamsProtocol

	err = runtime.BindStyledParameterWithOptions("simple", "protocol", r.PathValue("protocol"), &protocol, runtime.BindStyledParameterOptions{ParamLocation: runtime.ParamLocationPath, Explode: false, Required: true})
	if err != nil {
		siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: "protocol", Err: err})
		return
	}

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siw.Handler.PostPublishProtocol(w, r, protocol)
	}))

	for _, middleware := range siw.HandlerMiddlewares {
		handler = middleware(handler)
	}

	handler.ServeHTTP(w, r)
}

type UnescapedCookieParamError struct {
	ParamName string
	Err       error
}

func (e *UnescapedCookieParamError) Error() string {
	return fmt.Sprintf("error unescaping cookie parameter '%s'", e.ParamName)
}

func (e *UnescapedCookieParamError) Unwrap() error {
	return e.Err
}

type UnmarshalingParamError struct {
	ParamName string
	Err       error
}

func (e *UnmarshalingParamError) Error() string {
	return fmt.Sprintf("Error unmarshaling parameter %s as JSON: %s", e.ParamName, e.Err.Error())
}

func (e *UnmarshalingParamError) Unwrap() error {
	return e.Err
}

type RequiredParamError struct {
	ParamName string
}

func (e *RequiredParamError) Error() string {
	return fmt.Sprintf("Query argument %s is required, but not found", e.ParamName)
}

type RequiredHeaderError struct {
	ParamName string
	Err       error
}

func (e *RequiredHeaderError) Error() string {
	return fmt.Sprintf("Header parameter %s is required, but not found", e.ParamName)
}

func (e *RequiredHeaderError) Unwrap() error {
	return e.Err
}

type InvalidParamFormatError struct {
	ParamName string
	Err       error
}

func (e *InvalidParamFormatError) Error() string {
	return fmt.Sprintf("Invalid format for parameter %s: %s", e.ParamName, e.Err.Error())
}

func (e *InvalidParamFormatError) Unwrap() error {
	return e.Err
}

type TooManyValuesForParamError struct {
	ParamName string
	Count     int
}

func (e *TooManyValuesForParamError) Error() string {
	return fmt.Sprintf("Expected one value for %s, got %d", e.ParamName, e.Count)
}

// Handler creates http.Handler with routing matching OpenAPI spec.
func Handler(si ServerInterface) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{})
}

// ServeMux is an abstraction of http.ServeMux.
type ServeMux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type StdHTTPServerOptions struct {
	BaseURL          string
	BaseRouter       ServeMux
	Middlewares      []MiddlewareFunc
	ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

// HandlerFromMux creates http.Handler with routing matching OpenAPI spec based on the provided mux.
func HandlerFromMux(si ServerInterface, m ServeMux) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{
		BaseRouter: m,
	})
}

func HandlerFromMuxWithBaseURL(si ServerInterface, m ServeMux, baseURL string) http.Handler {
	return HandlerWithOptions(si, StdHTTPServerOptions{
		BaseURL:    baseURL,
		BaseRouter: m,
	})
}

// HandlerWithOptions creates http.Handler with additional options
func HandlerWithOptions(si ServerInterface, options StdHTTPServerOptions) http.Handler {
	m := options.BaseRouter

	if m == nil {
		m = http.NewServeMux()
	}
	if options.ErrorHandlerFunc == nil {
		options.ErrorHandlerFunc = func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}

	wrapper := ServerInterfaceWrapper{
		Handler:            si,
		HandlerMiddlewares: options.Middlewares,
		ErrorHandlerFunc:   options.ErrorHandlerFunc,
	}

	m.HandleFunc("GET "+options.BaseURL+"/activation/atx/{atx_id}", wrapper.GetActivationAtxAtxId)
	m.HandleFunc("GET "+options.BaseURL+"/activation/last_atx/{node_id}", wrapper.GetActivationLastAtxNodeId)
	m.HandleFunc("GET "+options.BaseURL+"/activation/positioning_atx/{publish_epoch}", wrapper.GetActivationPositioningAtxPublishEpoch)
	m.HandleFunc("POST "+options.BaseURL+"/hare/publish", wrapper.PostHarePublish)
	m.HandleFunc("GET "+options.BaseURL+"/hare/round_template/{layer}/{round}", wrapper.GetHareRoundTemplateLayerRound)
	m.HandleFunc("POST "+options.BaseURL+"/poet", wrapper.PostPoet)
	m.HandleFunc("POST "+options.BaseURL+"/publish/{protocol}", wrapper.PostPublishProtocol)

	return m
}

type GetActivationAtxAtxIdRequestObject struct {
	AtxId externalRef0.ATXID `json:"atx_id"`
}

type GetActivationAtxAtxIdResponseObject interface {
	VisitGetActivationAtxAtxIdResponse(w http.ResponseWriter) error
}

type GetActivationAtxAtxId200JSONResponse externalRef0.ActivationTx

func (response GetActivationAtxAtxId200JSONResponse) VisitGetActivationAtxAtxIdResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetActivationAtxAtxId404Response struct {
}

func (response GetActivationAtxAtxId404Response) VisitGetActivationAtxAtxIdResponse(w http.ResponseWriter) error {
	w.WriteHeader(404)
	return nil
}

type GetActivationLastAtxNodeIdRequestObject struct {
	NodeId externalRef0.NodeID `json:"node_id"`
}

type GetActivationLastAtxNodeIdResponseObject interface {
	VisitGetActivationLastAtxNodeIdResponse(w http.ResponseWriter) error
}

type GetActivationLastAtxNodeId200JSONResponse externalRef0.ActivationTx

func (response GetActivationLastAtxNodeId200JSONResponse) VisitGetActivationLastAtxNodeIdResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type GetActivationLastAtxNodeId400PlaintextResponse struct {
	Body          io.Reader
	ContentLength int64
}

func (response GetActivationLastAtxNodeId400PlaintextResponse) VisitGetActivationLastAtxNodeIdResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "plain/text")
	if response.ContentLength != 0 {
		w.Header().Set("Content-Length", fmt.Sprint(response.ContentLength))
	}
	w.WriteHeader(400)

	if closer, ok := response.Body.(io.ReadCloser); ok {
		defer closer.Close()
	}
	_, err := io.Copy(w, response.Body)
	return err
}

type GetActivationLastAtxNodeId404Response struct {
}

func (response GetActivationLastAtxNodeId404Response) VisitGetActivationLastAtxNodeIdResponse(w http.ResponseWriter) error {
	w.WriteHeader(404)
	return nil
}

type GetActivationPositioningAtxPublishEpochRequestObject struct {
	PublishEpoch externalRef0.EpochID `json:"publish_epoch"`
}

type GetActivationPositioningAtxPublishEpochResponseObject interface {
	VisitGetActivationPositioningAtxPublishEpochResponse(w http.ResponseWriter) error
}

type GetActivationPositioningAtxPublishEpoch200JSONResponse struct {
	ID externalRef0.ATXID `json:"ID"`
}

func (response GetActivationPositioningAtxPublishEpoch200JSONResponse) VisitGetActivationPositioningAtxPublishEpochResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)

	return json.NewEncoder(w).Encode(response)
}

type PostHarePublishRequestObject struct {
	Body io.Reader
}

type PostHarePublishResponseObject interface {
	VisitPostHarePublishResponse(w http.ResponseWriter) error
}

type PostHarePublish202Response struct {
}

func (response PostHarePublish202Response) VisitPostHarePublishResponse(w http.ResponseWriter) error {
	w.WriteHeader(202)
	return nil
}

type PostHarePublish500Response struct {
}

func (response PostHarePublish500Response) VisitPostHarePublishResponse(w http.ResponseWriter) error {
	w.WriteHeader(500)
	return nil
}

type GetHareRoundTemplateLayerRoundRequestObject struct {
	Layer externalRef0.LayerID   `json:"layer"`
	Round externalRef0.HareRound `json:"round"`
}

type GetHareRoundTemplateLayerRoundResponseObject interface {
	VisitGetHareRoundTemplateLayerRoundResponse(w http.ResponseWriter) error
}

type GetHareRoundTemplateLayerRound200ApplicationoctetStreamResponse struct {
	Body          io.Reader
	ContentLength int64
}

func (response GetHareRoundTemplateLayerRound200ApplicationoctetStreamResponse) VisitGetHareRoundTemplateLayerRoundResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/octet-stream")
	if response.ContentLength != 0 {
		w.Header().Set("Content-Length", fmt.Sprint(response.ContentLength))
	}
	w.WriteHeader(200)

	if closer, ok := response.Body.(io.ReadCloser); ok {
		defer closer.Close()
	}
	_, err := io.Copy(w, response.Body)
	return err
}

type GetHareRoundTemplateLayerRound204Response struct {
}

func (response GetHareRoundTemplateLayerRound204Response) VisitGetHareRoundTemplateLayerRoundResponse(w http.ResponseWriter) error {
	w.WriteHeader(204)
	return nil
}

type PostPoetRequestObject struct {
	Body io.Reader
}

type PostPoetResponseObject interface {
	VisitPostPoetResponse(w http.ResponseWriter) error
}

type PostPoet200Response struct {
}

func (response PostPoet200Response) VisitPostPoetResponse(w http.ResponseWriter) error {
	w.WriteHeader(200)
	return nil
}

type PostPoet400PlaintextResponse struct {
	Body          io.Reader
	ContentLength int64
}

func (response PostPoet400PlaintextResponse) VisitPostPoetResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "plain/text")
	if response.ContentLength != 0 {
		w.Header().Set("Content-Length", fmt.Sprint(response.ContentLength))
	}
	w.WriteHeader(400)

	if closer, ok := response.Body.(io.ReadCloser); ok {
		defer closer.Close()
	}
	_, err := io.Copy(w, response.Body)
	return err
}

type PostPublishProtocolRequestObject struct {
	Protocol PostPublishProtocolParamsProtocol `json:"protocol"`
	Body     io.Reader
}

type PostPublishProtocolResponseObject interface {
	VisitPostPublishProtocolResponse(w http.ResponseWriter) error
}

type PostPublishProtocol200Response struct {
}

func (response PostPublishProtocol200Response) VisitPostPublishProtocolResponse(w http.ResponseWriter) error {
	w.WriteHeader(200)
	return nil
}

// StrictServerInterface represents all server handlers.
type StrictServerInterface interface {
	// Get ATX by ID
	// (GET /activation/atx/{atx_id})
	GetActivationAtxAtxId(ctx context.Context, request GetActivationAtxAtxIdRequestObject) (GetActivationAtxAtxIdResponseObject, error)
	// Get last ATX by node ID
	// (GET /activation/last_atx/{node_id})
	GetActivationLastAtxNodeId(ctx context.Context, request GetActivationLastAtxNodeIdRequestObject) (GetActivationLastAtxNodeIdResponseObject, error)
	// Get Positioning ATX ID with given maximum publish epoch
	// (GET /activation/positioning_atx/{publish_epoch})
	GetActivationPositioningAtxPublishEpoch(ctx context.Context, request GetActivationPositioningAtxPublishEpochRequestObject) (GetActivationPositioningAtxPublishEpochResponseObject, error)
	// Publish a signed hare message
	// (POST /hare/publish)
	PostHarePublish(ctx context.Context, request PostHarePublishRequestObject) (PostHarePublishResponseObject, error)
	// Get a hare message to sign
	// (GET /hare/round_template/{layer}/{round})
	GetHareRoundTemplateLayerRound(ctx context.Context, request GetHareRoundTemplateLayerRoundRequestObject) (GetHareRoundTemplateLayerRoundResponseObject, error)
	// Store PoET proof
	// (POST /poet)
	PostPoet(ctx context.Context, request PostPoetRequestObject) (PostPoetResponseObject, error)
	// Publish a blob in the given p2p protocol
	// (POST /publish/{protocol})
	PostPublishProtocol(ctx context.Context, request PostPublishProtocolRequestObject) (PostPublishProtocolResponseObject, error)
}

type StrictHandlerFunc = strictnethttp.StrictHTTPHandlerFunc
type StrictMiddlewareFunc = strictnethttp.StrictHTTPMiddlewareFunc

type StrictHTTPServerOptions struct {
	RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
	ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
}

func NewStrictHandler(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc) ServerInterface {
	return &strictHandler{ssi: ssi, middlewares: middlewares, options: StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		},
	}}
}

func NewStrictHandlerWithOptions(ssi StrictServerInterface, middlewares []StrictMiddlewareFunc, options StrictHTTPServerOptions) ServerInterface {
	return &strictHandler{ssi: ssi, middlewares: middlewares, options: options}
}

type strictHandler struct {
	ssi         StrictServerInterface
	middlewares []StrictMiddlewareFunc
	options     StrictHTTPServerOptions
}

// GetActivationAtxAtxId operation middleware
func (sh *strictHandler) GetActivationAtxAtxId(w http.ResponseWriter, r *http.Request, atxId externalRef0.ATXID) {
	var request GetActivationAtxAtxIdRequestObject

	request.AtxId = atxId

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetActivationAtxAtxId(ctx, request.(GetActivationAtxAtxIdRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetActivationAtxAtxId")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetActivationAtxAtxIdResponseObject); ok {
		if err := validResponse.VisitGetActivationAtxAtxIdResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// GetActivationLastAtxNodeId operation middleware
func (sh *strictHandler) GetActivationLastAtxNodeId(w http.ResponseWriter, r *http.Request, nodeId externalRef0.NodeID) {
	var request GetActivationLastAtxNodeIdRequestObject

	request.NodeId = nodeId

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetActivationLastAtxNodeId(ctx, request.(GetActivationLastAtxNodeIdRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetActivationLastAtxNodeId")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetActivationLastAtxNodeIdResponseObject); ok {
		if err := validResponse.VisitGetActivationLastAtxNodeIdResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// GetActivationPositioningAtxPublishEpoch operation middleware
func (sh *strictHandler) GetActivationPositioningAtxPublishEpoch(w http.ResponseWriter, r *http.Request, publishEpoch externalRef0.EpochID) {
	var request GetActivationPositioningAtxPublishEpochRequestObject

	request.PublishEpoch = publishEpoch

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetActivationPositioningAtxPublishEpoch(ctx, request.(GetActivationPositioningAtxPublishEpochRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetActivationPositioningAtxPublishEpoch")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetActivationPositioningAtxPublishEpochResponseObject); ok {
		if err := validResponse.VisitGetActivationPositioningAtxPublishEpochResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// PostHarePublish operation middleware
func (sh *strictHandler) PostHarePublish(w http.ResponseWriter, r *http.Request) {
	var request PostHarePublishRequestObject

	request.Body = r.Body

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.PostHarePublish(ctx, request.(PostHarePublishRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "PostHarePublish")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(PostHarePublishResponseObject); ok {
		if err := validResponse.VisitPostHarePublishResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// GetHareRoundTemplateLayerRound operation middleware
func (sh *strictHandler) GetHareRoundTemplateLayerRound(w http.ResponseWriter, r *http.Request, layer externalRef0.LayerID, round externalRef0.HareRound) {
	var request GetHareRoundTemplateLayerRoundRequestObject

	request.Layer = layer
	request.Round = round

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.GetHareRoundTemplateLayerRound(ctx, request.(GetHareRoundTemplateLayerRoundRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "GetHareRoundTemplateLayerRound")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(GetHareRoundTemplateLayerRoundResponseObject); ok {
		if err := validResponse.VisitGetHareRoundTemplateLayerRoundResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// PostPoet operation middleware
func (sh *strictHandler) PostPoet(w http.ResponseWriter, r *http.Request) {
	var request PostPoetRequestObject

	request.Body = r.Body

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.PostPoet(ctx, request.(PostPoetRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "PostPoet")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(PostPoetResponseObject); ok {
		if err := validResponse.VisitPostPoetResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}

// PostPublishProtocol operation middleware
func (sh *strictHandler) PostPublishProtocol(w http.ResponseWriter, r *http.Request, protocol PostPublishProtocolParamsProtocol) {
	var request PostPublishProtocolRequestObject

	request.Protocol = protocol

	request.Body = r.Body

	handler := func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		return sh.ssi.PostPublishProtocol(ctx, request.(PostPublishProtocolRequestObject))
	}
	for _, middleware := range sh.middlewares {
		handler = middleware(handler, "PostPublishProtocol")
	}

	response, err := handler(r.Context(), w, r, request)

	if err != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, err)
	} else if validResponse, ok := response.(PostPublishProtocolResponseObject); ok {
		if err := validResponse.VisitPostPublishProtocolResponse(w); err != nil {
			sh.options.ResponseErrorHandlerFunc(w, r, err)
		}
	} else if response != nil {
		sh.options.ResponseErrorHandlerFunc(w, r, fmt.Errorf("unexpected response type: %T", response))
	}
}
