import { useCallback, useEffect, useState } from "react";
import { ApiKeyInput } from "./components/ApiKeyInput";
import { LinkForm } from "./components/LinkForm";
import { LinkRow } from "./components/LinkRow";
import { createLink, deleteLink, listLinks, updateLink } from "./api";
import type { LinkDraft, LinkResponse } from "./types";

type Modal =
  | { kind: "none" }
  | { kind: "create" }
  | { kind: "edit"; link: LinkResponse }
  | { kind: "delete"; link: LinkResponse };

const PATCH_KEYS = [
  "url",
  "title",
  "description",
  "image_url",
  "image_width",
  "image_height",
  "image_alt",
  "og_type",
  "locale",
] as const satisfies readonly (keyof LinkDraft)[];

// buildPatch returns only the fields that actually changed. Sending the
// whole draft would re-write equal fields and (for empty strings) clear
// values the user didn't intend to touch.
function buildPatch(link: LinkResponse, draft: LinkDraft): Partial<LinkDraft> {
  const patch: Partial<LinkDraft> = {};
  for (const key of PATCH_KEYS) {
    if (draft[key] !== link[key]) {
      Object.assign(patch, { [key]: draft[key] });
    }
  }
  return patch;
}

export default function App() {
  const [links, setLinks] = useState<LinkResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [modal, setModal] = useState<Modal>({ kind: "none" });
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await listLinks();
      setLinks(data);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const totalClicks = links.reduce((sum, l) => sum + l.clicks, 0);

  const handleCreate = async (draft: LinkDraft) => {
    setBusy(true);
    setError(null);
    try {
      await createLink(draft);
      setModal({ kind: "none" });
      await reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleEdit = async (draft: LinkDraft) => {
    if (modal.kind !== "edit") return;
    setBusy(true);
    setError(null);
    try {
      const patch = buildPatch(modal.link, draft);
      const id = modal.link.short_id;
      if (!id) throw new Error("missing short id");
      await updateLink(id, patch);
      setModal({ kind: "none" });
      await reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async () => {
    if (modal.kind !== "delete") return;
    setBusy(true);
    setError(null);
    try {
      const id = modal.link.short_id;
      if (!id) throw new Error("missing short id");
      await deleteLink(id);
      setModal({ kind: "none" });
      await reload();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <main>
      <header className="brand">
        <div className="brand-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
            <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
          </svg>
        </div>
        <h1>deeplink</h1>
      </header>
      <p className="subtitle">
        {loading ? "loading…" : `${links.length} links · ${totalClicks} clicks`}
      </p>

      {error && <div className="error-banner">{error}</div>}

      <div className="toolbar">
        <button className="btn btn-primary" onClick={() => setModal({ kind: "create" })}>
          + New link
        </button>
        <button className="btn btn-ghost" onClick={() => void reload()} disabled={loading}>
          Refresh
        </button>
        <ApiKeyInput />
      </div>

      <div className="link-table">
        {links.length === 0 && !loading ? (
          <div className="empty-state">No links yet. Create one with the button above.</div>
        ) : (
          links.map((link) => (
            <LinkRow
              key={link.short_id}
              link={link}
              onEdit={(l) => setModal({ kind: "edit", link: l })}
              onDelete={(l) => setModal({ kind: "delete", link: l })}
            />
          ))
        )}
      </div>

      {modal.kind === "create" && (
        <Modal title="Create link" onClose={() => setModal({ kind: "none" })}>
          <LinkForm
            mode="create"
            busy={busy}
            onSubmit={handleCreate}
            onCancel={() => setModal({ kind: "none" })}
          />
        </Modal>
      )}

      {modal.kind === "edit" && (
        <Modal title="Edit link" onClose={() => setModal({ kind: "none" })}>
          <LinkForm
            mode="edit"
            busy={busy}
            initial={modal.link}
            onSubmit={handleEdit}
            onCancel={() => setModal({ kind: "none" })}
          />
        </Modal>
      )}

      {modal.kind === "delete" && (
        <Modal title="Delete link?" onClose={() => setModal({ kind: "none" })}>
          <p>
            Delete <code>{modal.link.short_id}</code>? This soft-deletes the link;
            it stays recoverable in the backing store for the grace window.
          </p>
          <div className="modal-actions">
            <button
              type="button"
              className="btn"
              onClick={() => setModal({ kind: "none" })}
              disabled={busy}
            >
              Cancel
            </button>
            <button
              type="button"
              className="btn btn-danger"
              onClick={() => void handleDelete()}
              disabled={busy}
            >
              {busy ? "Deleting…" : "Delete"}
            </button>
          </div>
        </Modal>
      )}

      <footer className="site-footer">
        powered by{" "}
        <a
          href="https://github.com/yinebebt/deeplink"
          target="_blank"
          rel="noreferrer noopener"
        >
          deeplink
        </a>
      </footer>
    </main>
  );
}

function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>{title}</h2>
        {children}
      </div>
    </div>
  );
}
