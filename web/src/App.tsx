import { useEffect, useState } from "react";
import Landing from "./components/Landing";
import Dashboard from "./components/Dashboard";
import { SiteHeader } from "./components/SiteHeader";
import { SiteFooter } from "./components/SiteFooter";

// "#/app" → dashboard; anything else → landing.
function route(): "app" | "home" {
  return window.location.hash.replace(/^#/, "").startsWith("/app") ? "app" : "home";
}

export default function App() {
  const [view, setView] = useState<"app" | "home">(route);

  useEffect(() => {
    const onHash = () => {
      const next = route();
      setView((prev) => {
        if (prev !== next) window.scrollTo({ top: 0, behavior: "auto" });
        return next;
      });
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  return (
    <div className="app-shell">
      <SiteHeader view={view === "app" ? "app" : "home"} />
      <div className="page-enter app-content" key={view}>
        {view === "app" ? <Dashboard /> : <Landing />}
      </div>
      <SiteFooter />
    </div>
  );
}
