/** Sends the browser to another site, which a test cannot follow, so it is a module of its own to stand in for. */
export function leaveTo(address: string) {
  window.location.assign(address);
}
