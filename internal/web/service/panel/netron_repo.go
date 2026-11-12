package panel

// panelRepo is the GitHub owner/repo the self-updater installs from. Pointing it
// at stock 3x-ui would replace this fork's binary on the next update.
const panelRepo = "mahdizo3181/netron-panel"

// githubAPIBase is the GitHub REST root the update check queries; tests point it at httptest.
var githubAPIBase = "https://api.github.com"

// devReleasePublished is false: release.yml publishes dev-latest only for upstream,
// so the dev channel would check and install a release that does not exist here.
const devReleasePublished = false
