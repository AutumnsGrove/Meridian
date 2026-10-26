// A single-page app served by Go: nothing is rendered or prerendered on a server, and the Go
// gateway answers every unknown path with build/index.html (docs/agent/04, T14).
export const ssr = false;
export const prerender = false;
