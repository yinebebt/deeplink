import { useEffect, useRef, useState } from "react";
import type { LinkResponse } from "../types";

interface Props {
  link: LinkResponse;
  onEdit: (link: LinkResponse) => void;
  onDelete: (link: LinkResponse) => void;
}

export function LinkRow({ link, onEdit, onDelete }: Props) {
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
    setOpen(false);
  };

  const title = link.title?.trim() || link.short_id || "(untitled)";
  const initial = (title[0] || "?").toUpperCase();

  return (
    <div className="link-row">
      <div className={`thumb ${link.image_url ? "" : "placeholder"}`}>
        {link.image_url ? (
          <img src={link.image_url} alt={link.image_alt ?? ""} loading="lazy" />
        ) : (
          <span>{initial}</span>
        )}
      </div>
      <div style={{ overflow: "hidden" }}>
        <div className="link-title" title={title}>
          {title}
        </div>
        <div className="link-meta" title={link.url}>
          <a href={link.short_link} target="_blank" rel="noreferrer">
            {link.short_id}
          </a>
          {" → "}
          {link.url}
        </div>
      </div>
      <div className="click-badge">{link.clicks}</div>
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
            <button onClick={copy}>{copied ? "Copied!" : "Copy short URL"}</button>
            <button
              onClick={() => {
                setOpen(false);
                onEdit(link);
              }}
            >
              Edit
            </button>
            <button
              className="danger"
              onClick={() => {
                setOpen(false);
                onDelete(link);
              }}
            >
              Delete
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
