/*
Package vodka implements a fast and unfancy HTTP server framework for Go (Golang).

Example:

	package main

	import (
	    "net/http"

	    "github.com/insionng/vodka"
	    "github.com/insionng/vodka/engine/standard"
	    "github.com/insionng/vodka/middleware"
	)

	// Handler
	func hello(c vodka.Context) error {
	    return c.String(http.StatusOK, "Hello, World!")
	}

	func main() {
	    // Vodka instance
	    e := vodka.New()

	    // Middleware
	    e.Use(middleware.Logger())
	    e.Use(middleware.Recover())

	    // Routes
	    e.GET("/", hello)

	    // Start server
	    e.Run(standard.New(":1323"))
	}

Learn more at https://vodka.insionng.com
*/
package vodka

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"reflect"
	"runtime"
	"sync"

	kontext "context"
	"github.com/insionng/vodka/engine"
	"github.com/insionng/vodka/log"
	glog "github.com/insionng/vodka/libraries/gommon/log"
)

type (
	// Vodka is the top-level framework instance.
	Vodka struct {
		server           engine.Server
		premiddleware    []MiddlewareFunc
		middleware       []MiddlewareFunc
		maxParam         *int
		notFoundHandler  HandlerFunc
		httpErrorHandler HTTPErrorHandler
		binder           Binder
		renderer         Renderer
		pool             sync.Pool
		debug            bool
		router           *Router
		logger           log.Logger
	}

	// Route contains a handler and information for matching against requests.
	Route struct {
		Method  string
		Path    string
		Handler string
	}

	// HTTPError represents an error that occurred while handling a request.
	HTTPError struct {
		Code    int
		Message string
	}

	// MiddlewareFunc defines a function to process middleware.
	MiddlewareFunc func(HandlerFunc) HandlerFunc

	// HandlerFunc defines a function to server HTTP requests.
	HandlerFunc func(Context) error

	// HTTPErrorHandler is a centralized HTTP error handler.
	HTTPErrorHandler func(error, Context)

	// Validator is the interface that wraps the Validate function.
	Validator interface {
		Validate() error
	}

	// Renderer is the interface that wraps the Render function.
	Renderer interface {
		Render(io.Writer, string, Context) error
	}
)

// HTTP methods
const (
	CONNECT = "CONNECT"
	DELETE  = "DELETE"
	GET     = "GET"
	HEAD    = "HEAD"
	OPTIONS = "OPTIONS"
	PATCH   = "PATCH"
	POST    = "POST"
	PUT     = "PUT"
	TRACE   = "TRACE"
)

// MIME types
const (
	MIMEApplicationJSON                  = "application/json"
	MIMEApplicationJSONCharsetUTF8       = MIMEApplicationJSON + "; " + charsetUTF8
	MIMEApplicationJavaScript            = "application/javascript"
	MIMEApplicationJavaScriptCharsetUTF8 = MIMEApplicationJavaScript + "; " + charsetUTF8
	MIMEApplicationXML                   = "application/xml"
	MIMEApplicationXMLCharsetUTF8        = MIMEApplicationXML + "; " + charsetUTF8
	MIMEApplicationForm                  = "application/x-www-form-urlencoded"
	MIMEApplicationProtobuf              = "application/protobuf"
	MIMEApplicationMsgpack               = "application/msgpack"
	MIMETextHTML                         = "text/html"
	MIMETextHTMLCharsetUTF8              = MIMETextHTML + "; " + charsetUTF8
	MIMETextPlain                        = "text/plain"
	MIMETextPlainCharsetUTF8             = MIMETextPlain + "; " + charsetUTF8
	MIMEMultipartForm                    = "multipart/form-data"
	MIMEOctetStream                      = "application/octet-stream"
)

const (
	charsetUTF8 = "charset=utf-8"
)

// Headers
const (
	HeaderAcceptEncoding                = "Accept-Encoding"
	HeaderAllow                         = "Allow"
	HeaderAuthorization                 = "Authorization"
	HeaderContentDisposition            = "Content-Disposition"
	HeaderContentEncoding               = "Content-Encoding"
	HeaderContentLength                 = "Content-Length"
	HeaderContentType                   = "Content-Type"
	HeaderCookie                        = "Cookie"
	HeaderSetCookie                     = "Set-Cookie"
	HeaderIfModifiedSince               = "If-Modified-Since"
	HeaderLastModified                  = "Last-Modified"
	HeaderLocation                      = "Location"
	HeaderUpgrade                       = "Upgrade"
	HeaderVary                          = "Vary"
	HeaderWWWAuthenticate               = "WWW-Authenticate"
	HeaderXForwardedProto               = "X-Forwarded-Proto"
	HeaderXHTTPMethodOverride           = "X-HTTP-Method-Override"
	HeaderXForwardedFor                 = "X-Forwarded-For"
	HeaderXRealIP                       = "X-Real-IP"
	HeaderServer                        = "Server"
	HeaderOrigin                        = "Origin"
	HeaderAccessControlRequestMethod    = "Access-Control-Request-Method"
	HeaderAccessControlRequestHeaders   = "Access-Control-Request-Headers"
	HeaderAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderAccessControlMaxAge           = "Access-Control-Max-Age"

	// Security
	HeaderStrictTransportSecurity = "Strict-Transport-Security"
	HeaderXContentTypeOptions     = "X-Content-Type-Options"
	HeaderXXSSProtection          = "X-XSS-Protection"
	HeaderXFrameOptions           = "X-Frame-Options"
	HeaderContentSecurityPolicy   = "Content-Security-Policy"
	HeaderXCSRFToken              = "X-CSRF-Token"
)

var (
	methods = [...]string{
		CONNECT,
		DELETE,
		GET,
		HEAD,
		OPTIONS,
		PATCH,
		POST,
		PUT,
		TRACE,
	}
)

// Errors
var (
	ErrUnsupportedMediaType        = NewHTTPError(http.StatusUnsupportedMediaType)
	ErrNotFound                    = NewHTTPError(http.StatusNotFound)
	ErrUnauthorized                = NewHTTPError(http.StatusUnauthorized)
	ErrMethodNotAllowed            = NewHTTPError(http.StatusMethodNotAllowed)
	ErrStatusRequestEntityTooLarge = NewHTTPError(http.StatusRequestEntityTooLarge)
	ErrRendererNotRegistered       = errors.New("renderer not registered")
	ErrInvalidRedirectCode         = errors.New("invalid redirect status code")
	ErrCookieNotFound              = errors.New("cookie not found")
)

// Error handlers
var (
	NotFoundHandler = func(c Context) error {
		return ErrNotFound
	}

	MethodNotAllowedHandler = func(c Context) error {
		return ErrMethodNotAllowed
	}
)

// New creates an instance of Vodka.
func New() (e *Vodka) {
	e = &Vodka{maxParam: new(int)}
	e.pool.New = func() interface{} {
		return e.NewContext(nil, nil)
	}
	e.router = NewRouter(e)

	// Defaults
	e.SetHTTPErrorHandler(e.DefaultHTTPErrorHandler)
	e.SetBinder(&binder{})
	l := glog.New("vodka")
	l.SetLevel(glog.OFF)
	e.SetLogger(l)
	return
}

// NewContext returns a Context instance.
func (e *Vodka) NewContext(req engine.Request, res engine.Response) Context {
	return &context{
		stdContext: kontext.Background(),
		request:    req,
		response:   res,
		store:      make(store),
		vodka:       e,
		pvalues:    make([]string, *e.maxParam),
		handler:    NotFoundHandler,
	}
}

// Router returns router.
func (e *Vodka) Router() *Router {
	return e.router
}

// Logger returns the logger instance.
func (e *Vodka) Logger() log.Logger {
	return e.logger
}

// SetLogger defines a custom logger.
func (e *Vodka) SetLogger(l log.Logger) {
	e.logger = l
}

// SetLogOutput sets the output destination for the logger. Default value is `os.Std*`
func (e *Vodka) SetLogOutput(w io.Writer) {
	e.logger.SetOutput(w)
}

// SetLogLevel sets the log level for the logger. Default value ERROR.
func (e *Vodka) SetLogLevel(l glog.Lvl) {
	e.logger.SetLevel(l)
}

// DefaultHTTPErrorHandler invokes the default HTTP error handler.
func (e *Vodka) DefaultHTTPErrorHandler(err error, c Context) {
	code := http.StatusInternalServerError
	msg := http.StatusText(code)
	if he, ok := err.(*HTTPError); ok {
		code = he.Code
		msg = he.Message
	}
	if e.debug {
		msg = err.Error()
	}
	if !c.Response().Committed() {
		if c.Request().Method() == HEAD { // Issue #608
			c.NoContent(code)
		} else {
			c.String(code, msg)
		}
	}
	e.logger.Error(err)
}

// SetHTTPErrorHandler registers a custom Vodka.HTTPErrorHandler.
func (e *Vodka) SetHTTPErrorHandler(h HTTPErrorHandler) {
	e.httpErrorHandler = h
}

// SetBinder registers a custom binder. It's invoked by `Context#Bind()`.
func (e *Vodka) SetBinder(b Binder) {
	e.binder = b
}

// Binder returns the binder instance.
func (e *Vodka) Binder() Binder {
	return e.binder
}

// SetRenderer registers an HTML template renderer. It's invoked by `Context#Render()`.
func (e *Vodka) SetRenderer(r Renderer) {
	e.renderer = r
}

// SetDebug enables/disables debug mode.
func (e *Vodka) SetDebug(on bool) {
	e.debug = on
}

// Debug returns debug mode (enabled or disabled).
func (e *Vodka) Debug() bool {
	return e.debug
}

// Pre adds middleware to the chain which is run before router.
func (e *Vodka) Pre(middleware ...MiddlewareFunc) {
	e.premiddleware = append(e.premiddleware, middleware...)
}

// Use adds middleware to the chain which is run after router.
func (e *Vodka) Use(middleware ...MiddlewareFunc) {
	e.middleware = append(e.middleware, middleware...)
}

// CONNECT registers a new CONNECT route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) CONNECT(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(CONNECT, path, h, m...)
}

// Connect is deprecated, use `CONNECT()` instead.
func (e *Vodka) Connect(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.CONNECT(path, h, m...)
}

// DELETE registers a new DELETE route for a path with matching handler in the router
// with optional route-level middleware.
func (e *Vodka) DELETE(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(DELETE, path, h, m...)
}

// Delete is deprecated, use `DELETE()` instead.
func (e *Vodka) Delete(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.DELETE(path, h, m...)
}

// GET registers a new GET route for a path with matching handler in the router
// with optional route-level middleware.
func (e *Vodka) GET(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(GET, path, h, m...)
}

// Get is deprecated, use `GET()` instead.
func (e *Vodka) Get(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.GET(path, h, m...)
}

// HEAD registers a new HEAD route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) HEAD(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(HEAD, path, h, m...)
}

// Head is deprecated, use `HEAD()` instead.
func (e *Vodka) Head(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.HEAD(path, h, m...)
}

// OPTIONS registers a new OPTIONS route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) OPTIONS(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(OPTIONS, path, h, m...)
}

// Options is deprecated, use `OPTIONS()` instead.
func (e *Vodka) Options(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.OPTIONS(path, h, m...)
}

// PATCH registers a new PATCH route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) PATCH(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(PATCH, path, h, m...)
}

// Patch is deprecated, use `PATCH()` instead.
func (e *Vodka) Patch(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.PATCH(path, h, m...)
}

// POST registers a new POST route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) POST(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(POST, path, h, m...)
}

// Post is deprecated, use `POST()` instead.
func (e *Vodka) Post(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.POST(path, h, m...)
}

// PUT registers a new PUT route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) PUT(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(PUT, path, h, m...)
}

// Put is deprecated, use `PUT()` instead.
func (e *Vodka) Put(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.PUT(path, h, m...)
}

// TRACE registers a new TRACE route for a path with matching handler in the
// router with optional route-level middleware.
func (e *Vodka) TRACE(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.add(TRACE, path, h, m...)
}

// Trace is deprecated, use `TRACE()` instead.
func (e *Vodka) Trace(path string, h HandlerFunc, m ...MiddlewareFunc) {
	e.TRACE(path, h, m...)
}

// Any registers a new route for all HTTP methods and path with matching handler
// in the router with optional route-level middleware.
func (e *Vodka) Any(path string, handler HandlerFunc, middleware ...MiddlewareFunc) {
	for _, m := range methods {
		e.add(m, path, handler, middleware...)
	}
}

// Match registers a new route for multiple HTTP methods and path with matching
// handler in the router with optional route-level middleware.
func (e *Vodka) Match(methods []string, path string, handler HandlerFunc, middleware ...MiddlewareFunc) {
	for _, m := range methods {
		e.add(m, path, handler, middleware...)
	}
}

// Static registers a new route with path prefix to serve static files from the
// provided root directory.
func (e *Vodka) Static(prefix, root string) {
	e.GET(prefix+"*", func(c Context) error {
		return c.File(path.Join(root, c.P(0)))
	})
}

// File registers a new route with path to serve a static file.
func (e *Vodka) File(path, file string) {
	e.GET(path, func(c Context) error {
		return c.File(file)
	})
}

func (e *Vodka) add(method, path string, handler HandlerFunc, middleware ...MiddlewareFunc) {
	name := handlerName(handler)
	e.router.Add(method, path, func(c Context) error {
		h := handler
		// Chain middleware
		for i := len(middleware) - 1; i >= 0; i-- {
			h = middleware[i](h)
		}
		return h(c)
	})
	r := Route{
		Method:  method,
		Path:    path,
		Handler: name,
	}
	e.router.routes[method+path] = r
}

// Group creates a new router group with prefix and optional group-level middleware.
func (e *Vodka) Group(prefix string, m ...MiddlewareFunc) (g *Group) {
	g = &Group{prefix: prefix, vodka: e}
	g.Use(m...)
	return
}

// URI generates a URI from handler.
func (e *Vodka) URI(handler HandlerFunc, params ...interface{}) string {
	uri := new(bytes.Buffer)
	ln := len(params)
	n := 0
	name := handlerName(handler)
	for _, r := range e.router.routes {
		if r.Handler == name {
			for i, l := 0, len(r.Path); i < l; i++ {
				if r.Path[i] == ':' && n < ln {
					for ; i < l && r.Path[i] != '/'; i++ {
					}
					uri.WriteString(fmt.Sprintf("%v", params[n]))
					n++
				}
				if i < l {
					uri.WriteByte(r.Path[i])
				}
			}
			break
		}
	}
	return uri.String()
}

// URL is an alias for `URI` function.
func (e *Vodka) URL(h HandlerFunc, params ...interface{}) string {
	return e.URI(h, params...)
}

// Routes returns the registered routes.
func (e *Vodka) Routes() []Route {
	routes := []Route{}
	for _, v := range e.router.routes {
		routes = append(routes, v)
	}
	return routes
}

// AcquireContext returns an empty `Context` instance from the pool.
// You must be return the context by calling `ReleaseContext()`.
func (e *Vodka) AcquireContext() Context {
	return e.pool.Get().(Context)
}

// ReleaseContext returns the `Context` instance back to the pool.
// You must call it after `AcquireContext()`.
func (e *Vodka) ReleaseContext(c Context) {
	e.pool.Put(c)
}

func (e *Vodka) ServeHTTP(req engine.Request, res engine.Response) {
	c := e.pool.Get().(*context)
	c.Reset(req, res)

	// Middleware
	h := func(c Context) error {
		method := req.Method()
		path := req.URL().Path()
		e.router.Find(method, path, c)
		h := c.Handler()
		for i := len(e.middleware) - 1; i >= 0; i-- {
			h = e.middleware[i](h)
		}
		return h(c)
	}

	// Premiddleware
	for i := len(e.premiddleware) - 1; i >= 0; i-- {
		h = e.premiddleware[i](h)
	}

	// Execute chain
	if err := h(c); err != nil {
		e.httpErrorHandler(err, c)
	}

	e.pool.Put(c)
}

// Run starts the HTTP server.
func (e *Vodka) Run(s engine.Server) error {
	e.server = s
	s.SetHandler(e)
	s.SetLogger(e.logger)
	if e.Debug() {
		e.SetLogLevel(glog.DEBUG)
		e.logger.Debug("running in debug mode")
	}
	return s.Start()
}

// Stop stops the HTTP server.
func (e *Vodka) Stop() error {
	return e.server.Stop()
}

// NewHTTPError creates a new HTTPError instance.
func NewHTTPError(code int, msg ...string) *HTTPError {
	he := &HTTPError{Code: code, Message: http.StatusText(code)}
	if len(msg) > 0 {
		m := msg[0]
		he.Message = m
	}
	return he
}

// Error makes it compatible with `error` interface.
func (e *HTTPError) Error() string {
	return e.Message
}

// WrapMiddleware wrap `vodka.HandlerFunc` into `vodka.MiddlewareFunc`.
func WrapMiddleware(h HandlerFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			if err := h(c); err != nil {
				return err
			}
			return next(c)
		}
	}
}

func handlerName(h HandlerFunc) string {
	t := reflect.ValueOf(h).Type()
	if t.Kind() == reflect.Func {
		return runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
	}
	return t.String()
}
