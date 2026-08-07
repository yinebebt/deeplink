const GITHUB = "https://github.com/yinebebt/deeplink";

export function SiteHeader({ view }: { view: "home" | "app" }) {
  return (
    <header className="site-header">
      <div className="site-header-inner">
        <a className="brand" href="#/">
          deeplink
        </a>
        <nav className="site-nav">
          <a href={GITHUB} target="_blank" rel="noreferrer noopener">
            GitHub
          </a>
          {view === "home" ? (
            <a href="#/app">Demo</a>
          ) : (
            <a href="#/">Home</a>
          )}
        </nav>
      </div>
    </header>
  );
}
