import { useEffect, useRef, useState, type ReactNode } from "react";

const GITHUB = "https://github.com/yinebebt/deeplink";

function Reveal({
  children,
  delay = 0,
  className = "",
}: {
  children: ReactNode;
  delay?: number;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const obs = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setShown(true);
          obs.disconnect();
        }
      },
      { threshold: 0.15 }
    );
    obs.observe(el);
    return () => obs.disconnect();
  }, []);

  return (
    <div
      ref={ref}
      className={`reveal ${shown ? "is-visible" : ""} ${className}`}
      style={{ transitionDelay: `${delay}ms` }}
    >
      {children}
    </div>
  );
}

const FEATURES = [
  {
    title: "Smart deep links",
    body: "One URL opens your iOS / Android app via Universal Links and App Links — and falls back to the store or web when the app isn't installed.",
  },
  {
    title: "Rich social previews",
    body: "Every link serves a preview page with Open Graph, Twitter Card, and fediverse tags, so shares render clean in any feed.",
  },
  {
    title: "Click analytics",
    body: "Every visit is captured off the redirect path — clicks by platform, referrer, and day, with no third-party tracker.",
  },
  {
    title: "Pluggable stores & expiry",
    body: "In-memory, Redis, or SQLite — same feature set. Set an expiry and links self-destruct. Core ships with one dependency.",
  },
];

const STEPS = [
  {
    n: "01",
    title: "Embed the library",
    body: "go get the package, register a processor, and mount the handler beside your own routes.",
  },
  {
    n: "02",
    title: "Create a link",
    body: "POST a destination and metadata; get back a short URL ready to share anywhere.",
  },
  {
    n: "03",
    title: "Share & measure",
    body: "Recipients land in the right app; you watch clicks roll in by platform and source.",
  },
];

export default function Landing() {
  return (
    <div className="landing">
      <section className="lp-hero">
        <div className="lp-hero-atmosphere" aria-hidden="true" />
        <div className="lp-hero-inner">
          <div className="lp-hero-copy">
            <h1 className="lp-title">Short links that open the right app.</h1>
            <p className="lp-lede">
              Self-hostable deep links for Go — store fallback, social previews,
              and click analytics.
            </p>
          </div>
          <div className="lp-terminal" aria-hidden="true">
            <div className="lp-terminal-bar">
              <span /><span /><span />
            </div>
            <pre className="lp-terminal-body">{`$ curl -X POST https://link.yinebebt.com/shorten \\
    -d '{"type":"redirect","url":"https://example.com/docs"}'

{ "short_url": "https://link.yinebebt.com/aBcD12" }`}
            </pre>
          </div>
        </div>

        <a className="lp-scroll-cue" href="#features" aria-label="Scroll to features">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </a>
      </section>

      <section className="lp-section" id="features">
        <Reveal>
          <p className="lp-kicker">What it does</p>
          <h2 className="lp-section-title">Everything a link should do</h2>
        </Reveal>
        <div className="lp-feature-grid">
          {FEATURES.map((f, i) => (
            <Reveal key={f.title} delay={i * 70}>
              <div className="lp-feature">
                <h3>{f.title}</h3>
                <p>{f.body}</p>
              </div>
            </Reveal>
          ))}
        </div>
      </section>

      <section className="lp-section lp-section-alt">
        <Reveal>
          <p className="lp-kicker">How it works</p>
          <h2 className="lp-section-title">Three steps to live</h2>
        </Reveal>
        <div className="lp-steps">
          {STEPS.map((s, i) => (
            <Reveal key={s.n} delay={i * 90}>
              <div className="lp-step">
                <span className="lp-step-num">{s.n}</span>
                <h3>{s.title}</h3>
                <p>{s.body}</p>
              </div>
            </Reveal>
          ))}
        </div>
        <Reveal delay={120}>
          <div className="lp-cta">
            <a className="btn btn-primary btn-lg" href="#/app">
              Try the live demo
            </a>
            <a className="btn btn-lg" href={GITHUB} target="_blank" rel="noreferrer noopener">
              View on GitHub
            </a>
          </div>
        </Reveal>
      </section>
    </div>
  );
}
