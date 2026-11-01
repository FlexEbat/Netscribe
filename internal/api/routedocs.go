package api

// RouteDoc is what the reference page says about one route. The route table gives the
// method, the path and the permission; this gives the words. A test keeps the two in step:
// a route without an entry, or an entry without a route, fails it.
type RouteDoc struct {
	Summary string
	// Query lists the query parameters, each as "name: what it does".
	Query []string
	// Body says what a request body holds, in a sentence.
	Body string
	// Answer says what comes back, in a sentence.
	Answer string
}

// routeDocs is keyed by "METHOD /path", the way the route table writes them.
var routeDocs = map[string]RouteDoc{
	"GET /healthz":                    {Summary: "Tells whether the service is up. No sign-in.", Answer: `{"status":"ok"}`},
	"POST /api/auth/login":            {Summary: "Signs in and sets the session cookie.", Body: `{"username","password"}; with a second factor (when set up) the answer asks for it.`, Answer: "The user, and the cookie."},
	"POST /api/auth/logout":           {Summary: "Ends the session."},
	"GET /api/auth/me":                {Summary: "The signed in user and what the caller may do.", Answer: "The user, the role and the permissions of the caller."},
	"PUT /api/auth/password":          {Summary: "Changes the password of the caller.", Body: `{"current","next"}`},
	"POST /api/auth/login/totp":       {Summary: "The second step of a sign in: the code from the authenticator app, or a recovery code.", Body: `{"challenge","code"}; the challenge comes from the answer of the login.`, Answer: "The user, and the cookie."},
	"GET /api/auth/totp":              {Summary: "Whether the second factor is on, and how many recovery codes are left.", Answer: `{"enabled","recoveryCodesLeft"}`},
	"POST /api/auth/totp/setup":       {Summary: "Starts the setup of a second factor. Nothing is on until a code is given to enable.", Body: `{"password"}`, Answer: `{"secret","uri"}: the secret to type and the otpauth address for a QR code.`},
	"POST /api/auth/totp/enable":      {Summary: "Turns the second factor on with a code of the new secret.", Body: `{"code"}`, Answer: `{"recoveryCodes"}: ten codes, shown once.`},
	"DELETE /api/auth/totp":           {Summary: "Turns the second factor off.", Body: `{"password","code"}; the code may be a recovery code.`},
	"POST /api/users/{id}/totp/reset": {Summary: "Removes the second factor of a person who lost it and ends that person's sessions."},
	"GET /api/auth/sessions":          {Summary: "Lists your own sessions, with the one of this request marked.", Answer: "A list with id, times, address, browser and current."},
	"DELETE /api/auth/sessions/{id}":  {Summary: "Ends one of your sessions. The session of another person is not found."},
	"DELETE /api/auth/sessions":       {Summary: "Ends all your sessions but the one of this request.", Answer: `{"ended": how many}`},
	"GET /api/version":                {Summary: "The running version and whether a newer release exists.", Answer: "Version, and the update state."},

	"GET /api/users":                  {Summary: "Lists the accounts.", Answer: "A list of users."},
	"POST /api/users":                 {Summary: "Creates an account.", Body: `{"username","role","password"}`},
	"PATCH /api/users/{id}":           {Summary: "Changes the role or the state of an account."},
	"PUT /api/users/{id}/password":    {Summary: "Sets a new password for another account.", Body: `{"password"}`},
	"DELETE /api/users/{id}":          {Summary: "Deletes an account. The last administrator cannot be deleted."},
	"GET /api/tokens":                 {Summary: "Lists your access tokens, never their secret part.", Answer: "A list of tokens with name, scope and dates."},
	"POST /api/tokens":                {Summary: "Creates an access token. The secret is shown once.", Body: `{"name","scope":"read"|"write","days"}`, Answer: "The token, with its secret."},
	"DELETE /api/tokens/{id}":         {Summary: "Revokes an access token."},
	"POST /api/update/check":          {Summary: "Looks for a newer release now."},
	"POST /api/update":                {Summary: "Asks the helper to install the newest release.", Answer: "202 when the helper took the request."},
	"POST /api/system/restart":        {Summary: "Asks the helper to restart the service.", Answer: "202 when the helper took the request."},
	"GET /api/health":                 {Summary: "The result of the last health checks, run the first time it is asked.", Answer: "The checks, when they ran, and the worst status."},
	"POST /api/health/run":            {Summary: "Runs the health checks again, at most once in 10 seconds.", Answer: "The same as GET /api/health; 429 when it is too soon."},
	"GET /api/backup":                 {Summary: "Downloads a backup: the database and the config with its secrets.", Query: []string{}, Answer: "A .tar.gz file. The password of the caller goes in the X-Confirm-Password header, UTF-8 and base64 encoded."},
	"POST /api/backup/restore":        {Summary: "Checks a backup and asks the helper to put it in place.", Body: "The .tar.gz file itself, with the password in the X-Confirm-Password header.", Answer: "202 and what the backup holds; the service restarts."},
	"GET /api/settings":               {Summary: "The settings, their values in use and which need a restart."},
	"PUT /api/settings":               {Summary: "Changes settings.", Body: "An object of setting names and new values."},
	"GET /api/notifications":          {Summary: "The notification channels and the events they are told about."},
	"POST /api/notifications/test":    {Summary: "Sends a test message to the channels."},
	"GET /api/audit":                  {Summary: "The audit log, newest first.", Query: []string{"limit: how many entries", "before: only entries older than this id", "result: only ok, denied or error"}},
	"GET /api/containers":             {Summary: "Containers found on the Docker host."},
	"GET /metrics":                    {Summary: "Counts of devices, risky services and scans in the Prometheus text format. Answers 404 unless metrics.enabled is on.", Answer: "text/plain; version=0.0.4"},
	"GET /api/export/{format}":        {Summary: "A document of the network in the given format.", Answer: "The file."},
	"GET /api/changes":                {Summary: "What changed between scans.", Query: []string{"limit: how many changes"}},
	"GET /api/devices":                {Summary: "Lists devices.", Query: []string{"q: text to look for", "kind: only this kind", "online: true or false", "trusted: true or false", "group: only this group id"}},
	"POST /api/devices":               {Summary: "Adds a device by hand."},
	"GET /api/devices/{id}":           {Summary: "One device with its services."},
	"GET /api/devices/{id}/history":   {Summary: "When the device was seen and what changed."},
	"PATCH /api/devices/{id}":         {Summary: "Changes the name, kind, notes, trust or group of a device."},
	"DELETE /api/devices/{id}":        {Summary: "Deletes a device."},
	"POST /api/devices/bulk":          {Summary: "Changes up to 200 devices at once.", Query: []string{"dryRun: true to only say what would change"}},
	"POST /api/devices/import":        {Summary: "Imports devices from CSV."},
	"PUT /api/devices/{id}/position":  {Summary: "Saves where a device sits on the topology canvas."},
	"POST /api/devices/{id}/identify": {Summary: "Asks one device again what it is."},
	"GET /api/overview":               {Summary: "Counts for the overview page.", Answer: "Devices by presence, risky services, recent scans."},
	"GET /api/groups":                 {Summary: "Lists groups."},
	"POST /api/groups":                {Summary: "Creates a group."},
	"PATCH /api/groups/{id}":          {Summary: "Renames or recolors a group."},
	"DELETE /api/groups/{id}":         {Summary: "Deletes a group."},
	"GET /api/filters":                {Summary: "Lists your saved filters."},
	"POST /api/filters":               {Summary: "Saves a filter. It is yours: it needs only the right to read."},
	"DELETE /api/filters/{id}":        {Summary: "Deletes one of your saved filters."},
	"POST /api/links":                 {Summary: "Draws a link between two devices by hand."},
	"DELETE /api/links/{id}":          {Summary: "Deletes a link made by hand."},
	"GET /api/topology":               {Summary: "Devices and links for the canvas."},
	"GET /api/services":               {Summary: "Open services.", Query: []string{"deviceId: only this device", "risky: true for services with a risk note"}},
	"GET /api/scans":                  {Summary: "Lists scans, newest first."},
	"GET /api/scans/{id}":             {Summary: "One scan."},
	"POST /api/scans":                 {Summary: "Starts a scan.", Answer: "202 and the scan that started."},
	"GET /api/nmap":                   {Summary: "Whether nmap is found and used."},
	"GET /api/events":                 {Summary: "A stream of events (server sent events): scans started and finished, devices changed."},
	"GET /api/openapi.json":           {Summary: "This reference as an OpenAPI 3.1 document, with only the routes the caller may use."},
}
