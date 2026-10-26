/**
 * Substitutes a search query into a configured URL template, replacing the single `{q}`. The query
 * is percent-encoded, so the result can carry anything the user typed and still parses as a URL.
 */
export function buildSearchURL(template: string, query: string): string {
	return template.replace('{q}', encodeURIComponent(query));
}
