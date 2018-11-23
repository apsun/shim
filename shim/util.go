package shim

import (
    "bytes"
    "io"
    "io/ioutil"
    "net/http"
    "strconv"
)

// Modifies the body of a request. f should read the body
// from in, and write the replacement body to out.
func ModifyRequestBody(r *http.Request, f func(in io.Reader, out io.Writer) error) error {
    buf := &bytes.Buffer{}
    err := f(r.Body, buf)
    if err != nil {
        return err
    }

    r.ContentLength = int64(buf.Len())
    r.Header.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
    r.Header.Del("Transfer-Encoding")
    r.Body = ioutil.NopCloser(buf)
    return nil
}

// Modifies the body of a response. f should read the body
// from in, and write the replacement body to out.
func ModifyResponseBody(r *http.Response, f func(in io.Reader, out io.Writer) error) error {
    buf := &bytes.Buffer{}
    err := f(r.Body, buf)
    if err != nil {
        return err
    }

    r.ContentLength = int64(buf.Len())
    r.Header.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
    r.Header.Del("Transfer-Encoding")
    r.Body = ioutil.NopCloser(buf)
    return nil
}
