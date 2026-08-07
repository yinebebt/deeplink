import { useState } from "react";
import type { LinkDraft } from "../types";

interface Props {
  initial?: LinkDraft;
  mode: "create" | "edit";
  busy?: boolean;
  error?: string | null;
  types?: string[];
  onSubmit: (draft: LinkDraft) => void | Promise<void>;
  onCancel: () => void;
}

const OG_TYPES = ["", "website", "article", "profile", "book", "video.movie"];

function toLocalInput(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

function fromLocalInput(local: string): string | undefined {
  if (!local) return undefined;
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

const CREATE_DEFAULTS: Partial<LinkDraft> = {
  type: "redirect",
  image_width: 1200,
  image_height: 630,
  og_type: "website",
};

function hasAdvancedValues(d: LinkDraft): boolean {
  return Boolean(
    d.image_url ||
      d.image_alt ||
      (d.og_type && d.og_type !== "website") ||
      d.locale ||
      (d.image_width && d.image_width !== CREATE_DEFAULTS.image_width) ||
      (d.image_height && d.image_height !== CREATE_DEFAULTS.image_height),
  );
}

export function LinkForm({ initial, mode, busy, error, types, onSubmit, onCancel }: Props) {
  const [draft, setDraft] = useState<LinkDraft>(
    initial ?? { ...CREATE_DEFAULTS },
  );
  const [advancedOpen, setAdvancedOpen] = useState(
    mode === "edit" && hasAdvancedValues(initial ?? {}),
  );

  const update = <K extends keyof LinkDraft>(key: K, value: LinkDraft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    void onSubmit(draft);
  };

  const typeOptions = types?.length ? types : [draft.type ?? "redirect"];

  return (
    <form onSubmit={submit}>
      {error && <div className="form-error">{error}</div>}

      {mode === "create" && (
        <div className="form-group">
          <label htmlFor="type">Type</label>
          <select
            id="type"
            value={draft.type ?? ""}
            onChange={(e) => update("type", e.target.value)}
            required
          >
            {typeOptions.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
      )}

      <div className="form-group">
        <label htmlFor="url">Destination URL</label>
        <input
          id="url"
          type="url"
          value={draft.url ?? ""}
          onChange={(e) => update("url", e.target.value)}
          placeholder="https://example.com/page"
          required
        />
      </div>

      <div className="form-group">
        <label htmlFor="title">Title</label>
        <input
          id="title"
          type="text"
          value={draft.title ?? ""}
          onChange={(e) => update("title", e.target.value)}
        />
      </div>

      <div className="form-group">
        <label htmlFor="description">Description</label>
        <textarea
          id="description"
          value={draft.description ?? ""}
          onChange={(e) => update("description", e.target.value)}
        />
      </div>

      {mode === "create" && (
        <div className="form-group">
          <label htmlFor="expires_at">Expires (optional)</label>
          <input
            id="expires_at"
            type="datetime-local"
            value={toLocalInput(draft.expires_at)}
            onChange={(e) => update("expires_at", fromLocalInput(e.target.value))}
          />
        </div>
      )}

      <button
        type="button"
        className="btn btn-ghost advanced-toggle"
        aria-expanded={advancedOpen}
        onClick={() => setAdvancedOpen((v) => !v)}
      >
        {advancedOpen ? "▾" : "▸"} Advanced (image, OG type, locale)
      </button>

      {advancedOpen && (
        <div className="advanced-fields">
          <div className="form-group">
            <label htmlFor="image_url">Image URL</label>
            <input
              id="image_url"
              type="url"
              value={draft.image_url ?? ""}
              onChange={(e) => update("image_url", e.target.value)}
              placeholder="https://cdn.example.com/cover.png"
            />
          </div>

          <div className="form-row">
            <div className="form-group">
              <label htmlFor="image_width">Width</label>
              <input
                id="image_width"
                type="number"
                min={0}
                value={draft.image_width ?? ""}
                onChange={(e) =>
                  update("image_width", e.target.value ? Number(e.target.value) : undefined)
                }
                placeholder="1200"
              />
            </div>
            <div className="form-group">
              <label htmlFor="image_height">Height</label>
              <input
                id="image_height"
                type="number"
                min={0}
                value={draft.image_height ?? ""}
                onChange={(e) =>
                  update("image_height", e.target.value ? Number(e.target.value) : undefined)
                }
                placeholder="630"
              />
            </div>
          </div>

          <div className="form-group">
            <label htmlFor="image_alt">Image alt</label>
            <input
              id="image_alt"
              type="text"
              value={draft.image_alt ?? ""}
              onChange={(e) => update("image_alt", e.target.value)}
            />
          </div>

          <div className="form-row">
            <div className="form-group">
              <label htmlFor="og_type">OG type</label>
              <select
                id="og_type"
                value={draft.og_type ?? ""}
                onChange={(e) => update("og_type", e.target.value)}
              >
                {OG_TYPES.map((t) => (
                  <option key={t} value={t}>
                    {t || "(default)"}
                  </option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <label htmlFor="locale">Locale</label>
              <input
                id="locale"
                type="text"
                value={draft.locale ?? ""}
                onChange={(e) => update("locale", e.target.value)}
                placeholder="en_US"
              />
            </div>
          </div>
        </div>
      )}

      <div className="modal-actions">
        <button type="button" className="btn" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? "Saving…" : mode === "create" ? "Create link" : "Save changes"}
        </button>
      </div>
    </form>
  );
}
