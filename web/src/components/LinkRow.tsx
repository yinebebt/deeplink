import { useEffect, useRef, useState } from "react";
import type { LinkResponse } from "../types";

interface Props {
  link: LinkResponse;
  onView: (link: LinkResponse) => void;
  onEdit: (link: LinkResponse) => void;
  onDelete: (link: LinkResponse) => void;
}

// prettyTarget renders only the host (+ first path segment) so long
// query strings don't blow out row layout. Full URL stays in the title
// tooltip and on the detail view.
function prettyTarget(raw: string): string {
  try {
    const u = new URL(raw);
    const segments = u.pathname.split("/").filter(Boolean);
    if (segments.length === 0) return u.host;
    return `${u.host}/${segments[0]}${segments.length > 1 || u.search ? "/…" : ""}`;
  } catch {
    return raw.length > 32 ? `${raw.slice(0, 32)}…` : raw;
  }
}

export function LinkRow({ link, onView, onEdit, onDelete }: Props) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link.short_link);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // ignore — clipboard may be blocked
    }
  };

  const title = link.title?.trim() || link.short_id || "(untitled)";
  const initial = (title[0] || "?").toUpperCase();

  return (
    <div className="link-row">
      <button
        type="button"
        className={`thumb ${link.image_url ? "" : "placeholder"}`}
        onClick={() => onView(link)}
        aria-label={`View details for ${title}`}
      >
        {link.image_url ? (
          <img src={link.image_url} alt={link.image_alt ?? ""} loading="lazy" />
        ) : (
          <span>{initial}</span>
        )}
      </button>
      <div className="link-body">
        <button
          type="button"
          className="link-title"
          title={title}
          onClick={() => onView(link)}
        >
          {title}
        </button>
        <div className="link-meta" title={link.url}>
          <a href={link.short_link} target="_blank" rel="noreferrer">
            {link.short_id}
          </a>
          <button
            type="button"
            className="copy-btn"
            aria-label={copied ? "Copied" : "Copy short URL"}
            title={copied ? "Copied!" : "Copy short URL"}
            onClick={copy}
          >
            {copied ? (
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="20 6 9 17 4 12" />
              </svg>
            ) : (
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
            )}
          </button>
          <span className="meta-sep">→</span>
          <span className="target-host">{prettyTarget(link.url ?? "")}</span>
        </div>
      </div>
      <div className="click-badge" title={`${link.clicks} clicks`}>{link.clicks}</div>
      <div className="dot-menu" ref={menuRef}>
        <button
          className="dot-btn"
          aria-label="Actions"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          ⋯
        </button>
        {open && (
          <div className="dot-popover" role="menu">
            <button onClick={() => { setOpen(false); onView(link); }}>
              View details
            </button>
            <button onClick={() => { setOpen(false); onEdit(link); }}>
              Edit
            </button>
            <button className="danger" onClick={() => { setOpen(false); onDelete(link); }}>
              Delete
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
