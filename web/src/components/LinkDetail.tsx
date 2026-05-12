import { useState } from "react";
import type { LinkResponse } from "../types";

interface Props {
  link: LinkResponse;
  onEdit: () => void;
  onDelete: () => void;
  onClose: () => void;
}

function formatDate(raw?: string): string | undefined {
  if (!raw) return undefined;
  const d = new Date(raw);
  return Number.isNaN(d.getTime()) ? raw : d.toLocaleString();
}

interface FieldProps {
  label: string;
  value?: React.ReactNode;
  mono?: boolean;
  wide?: boolean;
}

function Field({ label, value, mono, wide }: FieldProps) {
  const empty = value === undefined || value === null || value === "";
  const cls = ["detail-field", wide ? "wide" : "", mono ? "mono" : ""].filter(Boolean).join(" ");
  return (
    <div className={cls}>
      <div className="detail-label">{label}</div>
      <div className={`detail-value ${empty ? "empty" : ""}`}>
        {empty ? "—" : value}
      </div>
    </div>
  );
}

export function LinkDetail({ link, onEdit, onDelete, onClose }: Props) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link.short_link);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // ignore
    }
  };

  const title = link.title?.trim() || link.short_id || "(untitled)";

  return (
    <div className="link-detail">
      <div className="detail-head">
        <div className={`thumb large ${link.image_url ? "" : "placeholder"}`}>
          {link.image_url ? (
            <img src={link.image_url} alt={link.image_alt ?? ""} />
          ) : (
            <span>{(title[0] || "?").toUpperCase()}</span>
          )}
        </div>
        <div className="detail-head-text">
          <div className="detail-title">{title}</div>
          <div className="detail-clicks">{link.clicks} clicks</div>
        </div>
      </div>

      <div className="detail-short">
        <a href={link.short_link} target="_blank" rel="noreferrer">
          {link.short_link}
        </a>
        <button type="button" className="btn btn-ghost" onClick={copy}>
          {copied ? "Copied!" : "Copy"}
        </button>
      </div>

      <div className="detail-grid">
        <Field label="Type" value={link.type} mono />
        <Field label="Short ID" value={link.short_id} mono />
        <Field
          label="Destination"
          mono
          wide
          value={
            link.url && (
              <a href={link.url} target="_blank" rel="noreferrer" className="detail-link">
                {link.url}
              </a>
            )
          }
        />
        <Field label="Description" value={link.description} wide />
        <Field label="OG type" value={link.og_type} mono />
        <Field label="Locale" value={link.locale} mono />
        <Field label="Image URL" value={link.image_url} mono wide />
        <Field label="Image alt" value={link.image_alt} />
        <Field
          label="Image dimensions"
          mono
          value={
            link.image_width || link.image_height
              ? `${link.image_width ?? "?"} × ${link.image_height ?? "?"}`
              : undefined
          }
        />
        <Field label="Created" value={formatDate(link.created_at)} mono />
        <Field label="Updated" value={formatDate(link.updated_at)} mono />
        {link.deleted_at && <Field label="Deleted" value={formatDate(link.deleted_at)} mono />}
        {link.metadata && Object.keys(link.metadata).length > 0 && (
          <Field
            label="Metadata"
            wide
            value={<pre className="detail-meta">{JSON.stringify(link.metadata, null, 2)}</pre>}
          />
        )}
      </div>

      <div className="modal-actions">
        <button type="button" className="btn" onClick={onClose}>
          Close
        </button>
        <button type="button" className="btn btn-danger" onClick={onDelete}>
          Delete
        </button>
        <button type="button" className="btn btn-primary" onClick={onEdit}>
          Edit
        </button>
      </div>
    </div>
  );
}
