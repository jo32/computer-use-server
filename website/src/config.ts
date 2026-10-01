const repository = import.meta.env.VITE_REPOSITORY_URL || 'https://github.com/jo32/readyrig'

export const site = {
  repository,
  releases: import.meta.env.VITE_RELEASES_URL || `${repository}/releases/latest`,
  docs: import.meta.env.VITE_DOCS_URL || `${repository}#readme`,
  cliDocs: `${repository}#cli-and-vms`,
  cliInstallURL: import.meta.env.VITE_CLI_INSTALL_URL || 'https://readyrig.getmegaportal.com/install.sh',
  releasesRequireAccess: import.meta.env.VITE_RELEASES_REQUIRE_ACCESS === 'true',
  downloads: {
    arm64: import.meta.env.VITE_DOWNLOAD_MAC_ARM64 || `${repository}/releases/latest/download/readyrig-darwin-arm64.zip`,
    amd64: import.meta.env.VITE_DOWNLOAD_MAC_AMD64 || `${repository}/releases/latest/download/readyrig-darwin-amd64.zip`,
  },
}
