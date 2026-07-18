// First preload stage — registers happy-dom as globals. Kept in its own
// module (rather than merged into setup.ts) because ES module imports hoist
// and execute before any sibling statement in the *same* file: pulling in
// @testing-library/dom (which captures `document` once, at import time, into
// its `screen` singleton) from the same file as GlobalRegistrator.register()
// would run that capture before register() ever executes. Two preload
// entries in bunfig.toml load as separate module graphs in order, so this
// file's register() call is guaranteed to finish before setup.testing.ts's
// imports run.
import { GlobalRegistrator } from "@happy-dom/global-registrator";

// A concrete origin — utils/api's FetchEngine requires an absolute baseUrl
// (it resolves "/api" against window.location.origin), and happy-dom's
// default document location ("about:blank", origin "null") fails that.
GlobalRegistrator.register({ url: "http://localhost:4000/" });
