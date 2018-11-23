package shim

import (
    "io"
    "log"
    "net/http"
    "net/url"
    "strings"
    "golang.org/x/net/html"
)

// DowngradeHandler is responsible for taking HTTPS URLs from
// the real server and converting them into HTTP. It also converts
// the URLs back from HTTP to HTTPS when they are requested.
type DowngradeHandler struct{
    // Used to keep track of whether we've downgraded a URL.
    // TODO: Flush stale entries so we don't eventually OOM
    isHTTPS map[string]bool
}

// "Hashes" the host and path of a URL so we can determine
// in the future if it's been seen before. Since the query
// and fragment may be dynamically generated, we do not hash
// those portions of the URL.
func hashURL(u *url.URL) string {
    return u.Host + u.Path
}

// Returns whether the specified URL was downgraded from HTTPS
// to HTTP.
func (h *DowngradeHandler) isDowngradedURL(u *url.URL) bool {
    return h.isHTTPS[hashURL(u)]
}

// Undo a HTTPS -> HTTP downgrade. Used when proxying requests
// to the server for a resource that had its URL downgraded
// from a previous response.
func (h *DowngradeHandler) undowngradeURL(u *url.URL) {
    if h.isDowngradedURL(u) {
        u.Scheme = "https"
    }
}

// Converts a HTTPS link to a HTTP link, and remembers the link
// so that it can be upgraded back to HTTPS later when forwarding
// the request.
func (h *DowngradeHandler) downgradeURL(resp *http.Response, u *url.URL) error {
    isAbsURL := u.IsAbs()

    // First normalize the URL so we can properly hash it.
    // This converts relative URLs (e.g. foo, //abc.xyz/foo)
    // to absolute URLs (e.g. <a href="http://abc.xyz/foo">).
    newURL, err := resp.Request.URL.Parse(u.String())
    if err != nil {
        return err
    }

    // If HTTPS explicitly specified or using same-protocol
    // (e.g. //example.com) link and the base URL was downgraded,
    // replace it with an explicit HTTP link.
    if newURL.Scheme == "https" || !isAbsURL && h.isDowngradedURL(resp.Request.URL) {
        log.Printf("Downgrading URL: %s\n", newURL)
        h.isHTTPS[hashURL(newURL)] = true
        newURL.Scheme = "http"
    }

    *u = *newURL
    return nil
}

// Downgrades the Location header in a HTTP 300 redirect response.
func (h *DowngradeHandler) downgradeRedirect(resp *http.Response) error {
    // Let's not make the client cache this forever
    if resp.StatusCode == 301 {
        resp.StatusCode = 302
    }

    // Read header directly instead of using .Location() since
    // that will attempt to parse it relative to the page URL,
    // which means we will no longer be able to tell if it was
    // a relative redirect or a absolute redirect to a HTTP page.
    locationStr := resp.Header.Get("Location")
    if locationStr == "" {
        return nil
    }

    locationURL, err := url.Parse(locationStr)
    if err != nil {
        return err
    }

    // Replace location header. Note that clients are usually
    // okay with redirecting to the same URL, which can occur
    // if this is a HTTP -> HTTPS redirect. When they re-request
    // the page, our request handler will replace the HTTP with
    // the HTTPS URL.
    err = h.downgradeURL(resp, locationURL)
    if err != nil {
        return err
    }

    resp.Header.Set("Location", locationURL.String())
    return nil
}

// Partial list from https://stackoverflow.com/a/2725168
// Too lazy to get the rest; this list should cover 99%
// of use cases.
var tagsWithURLs = []struct {
    tag, attr string
} {
    {"a", "href"},
    {"script", "src"},
    {"link", "href"},
    {"img", "src"},
    {"iframe", "src"},
    {"form", "action"},
    {"input", "src"},
    {"body", "background"},
}

// Wrapper for ToLower, because I am lazy.
func lower(s string) string {
    return strings.ToLower(s)
}

// Returns whether the specified attribute of the specified
// tag is a URL and should be downgraded.
func isTagWithURL(n *html.Node, attr html.Attribute) bool {
    for _, t := range tagsWithURLs {
        if lower(n.Data) == t.tag && lower(attr.Key) == t.attr {
            return true
        }
    }
    return false
}

// Downgrades any URLs in a <meta http-equiv="refresh"> tag.
// This is commonly used to redirect HTTP -> HTTPS.
func (h *DowngradeHandler) downgradeHTMLMeta(resp *http.Response, n *html.Node) {
    // Find http-equiv attribute and check that this is a refresh tag
    refresh := false
    for _, attr := range n.Attr {
        if lower(attr.Key) == "http-equiv" && lower(attr.Val) == "refresh" {
            refresh = true
            break
        }
    }

    if !refresh {
        return
    }

    // Find content attribute. As per spec, it must be in the format
    // <time>;<one or more spaces>url=<url>, so we should be fine with
    // replacing everything after url=
    for i, attr := range n.Attr {
        if lower(attr.Key) == "content" {
            key := strings.Index(lower(attr.Val), "url=")
            if key < 0 {
                return
            }

            val := key + len("url=")
            u, err := url.Parse(attr.Val[val:])
            if err != nil {
                return
            }

            err = h.downgradeURL(resp, u)
            if err != nil {
                return
            }

            n.Attr[i].Val = attr.Val[:val] + u.String()
            return
        }
    }
}

// Recursive helper for downgradeHTML.
func (h *DowngradeHandler) downgradeHTMLHelper(resp *http.Response, n *html.Node) {
    // Downgrade all known URLs in the page
    if n.Type == html.ElementNode {
        if (n.Data == "meta") {
            h.downgradeHTMLMeta(resp, n)
        } else {
            for i, attr := range n.Attr {
                if isTagWithURL(n, attr) {
                    u, err := url.Parse(attr.Val)
                    if err != nil {
                        continue
                    }

                    err = h.downgradeURL(resp, u)
                    if err != nil {
                        continue
                    }

                    n.Attr[i].Val = u.String()
                }
            }
        }
    }

    // Recurse into children nodes
    for c := n.FirstChild; c != nil; c = c.NextSibling {
        h.downgradeHTMLHelper(resp, c)
    }
}

// Downgrades all URLs in a HTML page.
func (h *DowngradeHandler) downgradeHTML(resp *http.Response) error {
    // Skip non-HTML pages (this should catch text/html application/xhtml+xml)
    contentType := resp.Header.Get("Content-Type")
    if contentType != "" && !strings.Contains(contentType, "html") {
        return nil
    }

    return ModifyResponseBody(resp, func(in io.Reader, out io.Writer) error {
        doc, err := html.Parse(in)
        if err != nil {
            return err
        }
        h.downgradeHTMLHelper(resp, doc)
        return html.Render(out, doc)
    })
}

// Response handler. Downgrades all URLs in the response so the client
// will continue sending requests to us.
func (h *DowngradeHandler) OnResponse(resp *http.Response) error {
    if err := h.downgradeRedirect(resp); err != nil {
        return err
    }
    if err := h.downgradeHTML(resp); err != nil {
        return err
    }
    return nil
}

// Request handler. Un-downgrades the target URL so the server
// doesn't know about our shenanigans. If the target URL was actually
// originally HTTP, this will preserve it as HTTP.
func (h *DowngradeHandler) OnRequest(req *http.Request) error {
    h.undowngradeURL(req.URL)
    return nil
}

// Creates a new DowngradeHandler instance.
func NewDowngradeHandler() *DowngradeHandler {
    return &DowngradeHandler{
        isHTTPS: make(map[string]bool),
    }
}
