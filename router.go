package vodka

import "net/http"

type (
	Router struct {
		tree   *node
		routes []Route
		vodka  *Vodka
	}
	node struct {
		typ      ntype
		label    byte
		prefix   string
		parent   *node
		children children
		handler  map[string]HandlerFunc
		pnames   []string
		vodka    *Vodka
	}
	ntype    uint8
	children []*node
)

const (
	stype ntype = iota
	ptype
	mtype
)

func NewRouter(e *Vodka) *Router {
	return &Router{
		// tree is base node for the search tree for all routes, each node
		// therein contains a handler string->HandlerFunc map.  This allows
		// us to include the method applicable within the tree, allowing us to
		// detect if routes should not allow particular methods, and making the
		// router more clear
		tree: &node{
			handler: make(map[string]HandlerFunc),
		},
		routes: []Route{},
		vodka:  e,
	}
}

func (r *Router) Add(method, path string, h HandlerFunc, e *Vodka) {
	pnames := []string{} // Param names

	for i, l := 0, len(path); i < l; i++ {
		if path[i] == ':' {
			j := i + 1

			r.insert(method, path[:i], nil, stype, nil, e)
			for ; i < l && path[i] != '/'; i++ {
			}

			pnames = append(pnames, path[j:i])
			path = path[:j] + path[i:]
			i, l = j, len(path)

			if i == l {
				r.insert(method, path[:i], h, ptype, pnames, e)
				return
			}
			r.insert(method, path[:i], nil, ptype, pnames, e)
		} else if path[i] == '*' {
			r.insert(method, path[:i], nil, stype, nil, e)
			pnames = append(pnames, "_name")
			r.insert(method, path[:i+1], h, mtype, pnames, e)
			return
		}
	}

	r.insert(method, path, h, stype, pnames, e)
}

func (r *Router) insert(method, path string, h HandlerFunc, t ntype, pnames []string, e *Vodka) {
	// Adjust max param
	l := len(pnames)
	if *e.maxParam < l {
		*e.maxParam = l
	}

	cn := r.tree
	if !validMethod(method) {
		panic("vodka > invalid method")
	}
	search := path

	for {
		sl := len(search)
		pl := len(cn.prefix)
		l := 0

		// LCP
		max := pl
		if sl < max {
			max = sl
		}
		for ; l < max && search[l] == cn.prefix[l]; l++ {
		}

		if l == 0 {
			// At root node
			cn.label = search[0]
			cn.prefix = search
			if h != nil {
				cn.typ = t
				// handler is a map of methods to applicable handlers, map the inserted method to the
				// handler
				cn.handler = map[string]HandlerFunc{method: h}
				cn.pnames = pnames
				cn.vodka = e
			}
		} else if l < pl {
			// Split node
			n := newNode(cn.typ, cn.prefix[l:], cn, cn.children, cn.handler[method], cn.pnames, cn.vodka, method)

			// Reset parent node
			cn.typ = stype
			cn.label = cn.prefix[0]
			cn.prefix = cn.prefix[:l]
			cn.children = nil
			cn.handler = map[string]HandlerFunc{}
			cn.pnames = nil
			cn.vodka = nil

			cn.addChild(n)

			if l == sl {
				// At parent node
				cn.typ = t
				// add the handler to the node's map of methods to handlers
				cn.handler[method] = h
				cn.pnames = pnames
				cn.vodka = e
			} else {
				// Create child node
				n = newNode(t, search[l:], cn, nil, h, pnames, e, method)
				cn.addChild(n)
			}
		} else if l < sl {
			search = search[l:]
			c := cn.findChildWithLabel(search[0])
			if c != nil {
				// Go deeper
				cn = c
				continue
			}
			// Create child node
			n := newNode(t, search, cn, nil, h, pnames, e, method)
			cn.addChild(n)
		} else {
			// Node already exists
			if h != nil {
				// add the handler to the node's map of methods to handlers
				cn.handler[method] = h
				cn.pnames = pnames
				cn.vodka = e
			}
		}
		return
	}
}

// newNode - create a new router tree node
func newNode(t ntype, pre string, p *node, c children, h HandlerFunc, pnames []string, e *Vodka, m string) *node {
	return &node{
		typ:      t,
		label:    pre[0],
		prefix:   pre,
		parent:   p,
		children: c,
		// create a handler method to handler map for this node
		handler: map[string]HandlerFunc{m: h},
		pnames:  pnames,
		vodka:   e,
	}
}

func (n *node) addChild(c *node) {
	n.children = append(n.children, c)
}

func (n *node) findChild(l byte, t ntype) *node {
	for _, c := range n.children {
		if c.label == l && c.typ == t {
			return c
		}
	}
	return nil
}

func (n *node) findChildWithLabel(l byte) *node {
	for _, c := range n.children {
		if c.label == l {
			return c
		}
	}
	return nil
}

func (n *node) findChildWithType(t ntype) *node {
	for _, c := range n.children {
		if c.typ == t {
			return c
		}
	}
	return nil
}

//validMethod - validate that the http method is valid.
func validMethod(method string) bool {
	var ok = false
	for _, v := range methods {
		if v == method {
			ok = true
			break
		}
	}
	return ok
}

func (r *Router) Find(method, path string, ctx *Context) (h HandlerFunc, e *Vodka) {
	// get tree base node from the router
	cn := r.tree
	e = cn.vodka
	h = notFoundHandler

	if !validMethod(method) {
		// if the method is completely invalid
		allowedMethods := []string{}
		for m, _ := range cn.handler {
			allowedMethods = append(allowedMethods, m)
		}
		h = methodNotAllowedHandler(ctx, allowedMethods...)
		return
	}

	// Strip trailing slash
	if r.vodka.stripTrailingSlash {
		l := len(path)
		if path[l-1] == '/' {
			path = path[:l-1]
		}
	}

	var (
		search = path
		c      *node  // Child node
		n      int    // Param counter
		nt     ntype  // Next type
		nn     *node  // Next node
		ns     string // Next search
	)

	// TODO: Check empty path???

	// Search order static > param > match-any
	for {
		if search == "" {
			if cn.handler != nil {
				// Found route, check if method is applicable
				var ok = false
				h, ok = cn.handler[method]
				e = cn.vodka
				if !ok {
					// route is valid, but method is not allowed, 405
					allowedMethods := []string{}
					for m, _ := range cn.handler {
						allowedMethods = append(allowedMethods, m)
					}
					h = methodNotAllowedHandler(ctx, allowedMethods...)
					return
				}
				ctx.pnames = cn.pnames
				h = cn.handler[method]
			}
			return
		}

		pl := 0 // Prefix length
		l := 0  // LCP length

		if cn.label != ':' {
			sl := len(search)
			pl = len(cn.prefix)

			// LCP
			max := pl
			if sl < max {
				max = sl
			}
			for ; l < max && search[l] == cn.prefix[l]; l++ {
			}
		}

		if l == pl {
			// Continue search
			search = search[l:]
		} else {
			cn = nn
			search = ns
			if nt == ptype {
				goto Param
			} else if nt == mtype {
				goto MatchAny
			} else {
				// Not found
				return
			}
		}

		if search == "" {
			// TODO: Needs improvement
			if cn.findChildWithType(mtype) == nil {
				continue
			}
			// Empty value
			goto MatchAny
		}

		// Static node
		c = cn.findChild(search[0], stype)
		if c != nil {
			// Save next
			if cn.label == '/' {
				nt = ptype
				nn = cn
				ns = search
			}
			cn = c
			continue
		}

		// Param node
	Param:
		c = cn.findChildWithType(ptype)
		if c != nil {
			// Save next
			if cn.label == '/' {
				nt = mtype
				nn = cn
				ns = search
			}
			cn = c
			i, l := 0, len(search)
			for ; i < l && search[i] != '/'; i++ {
			}
			ctx.pvalues[n] = search[:i]
			n++
			search = search[i:]
			continue
		}

		// Match-any node
	MatchAny:
		//		c = cn.getChild()
		c = cn.findChildWithType(mtype)
		if c != nil {
			cn = c
			ctx.pvalues[0] = search
			search = "" // End search
			continue
		}

		// Not found
		return
	}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c := r.vodka.pool.Get().(*Context)
	h, _ := r.Find(req.Method, req.URL.Path, c)
	c.reset(req, w, r.vodka)
	if err := h(c); err != nil {
		r.vodka.httpErrorHandler(err, c)
	}
	r.vodka.pool.Put(c)
}
