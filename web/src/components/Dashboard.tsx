import { useCallback, useEffect, useState, type ReactNode } from "react";
import { ApiKeyInput } from "./ApiKeyInput";
import { LinkForm } from "./LinkForm";
import { LinkRow } from "./LinkRow";
import { LinkDetail } from "./LinkDetail";
import { createLink, deleteLink, listLinks, listTypes, updateLink } from "../api";
import type { LinkDraft, LinkResponse } from "../types";

type Modal =
  | { kind: "none" }
  | { kind: "create" }
  | { kind: "view"; link: LinkResponse }
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

// Only changed fields — empty strings in a full draft would clear untouched values.
function buildPatch(link: LinkResponse, draft: LinkDraft): Partial<LinkDraft> {
  const patch: Partial<LinkDraft> = {};
  for (const key of PATCH_KEYS) {
    if (draft[key] !== link[key]) {
      Object.assign(patch, { [key]: draft[key] });
    }
  }
  return patch;
}

export default function Dashboard() {
  const [links, setLinks] = useState<LinkResponse[]>([]);
  const [types, setTypes] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [modalError, setModalError] = useState<string | null>(null);
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
    listTypes().then(setTypes, () => {});
  }, [reload]);

  const totalClicks = links.reduce((sum, l) => sum + l.clicks, 0);

  const openModal = (m: Modal) => {
    setModalError(null);
    setModal(m);
  };

  const closeModal = () => {
    setModalError(null);
    setModal({ kind: "none" });
  };

  const handleCreate = async (draft: LinkDraft) => {
    setBusy(true);
    setModalError(null);
    try {
      await createLink(draft);
      closeModal();
      await reload();
    } catch (e) {
      setModalError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleEdit = async (draft: LinkDraft) => {
    if (modal.kind !== "edit") return;
    setBusy(true);
    setModalError(null);
    try {
      const patch = buildPatch(modal.link, draft);
      const id = modal.link.short_id;
      if (!id) throw new Error("missing short id");
      await updateLink(id, patch);
      closeModal();
      await reload();
    } catch (e) {
      setModalError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async () => {
    if (modal.kind !== "delete") return;
    setBusy(true);
    setModalError(null);
    try {
      const id = modal.link.short_id;
      if (!id) throw new Error("missing short id");
      await deleteLink(id);
      closeModal();
      await reload();
    } catch (e) {
      setModalError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <main>
      <p className="subtitle">
        {loading ? "loading…" : `${links.length} links · ${totalClicks} clicks`}
      </p>

      {error && <div className="error-banner">{error}</div>}

      <div className="toolbar">
        <button className="btn btn-primary" onClick={() => openModal({ kind: "create" })}>
          + New link
        </button>
        <button
          className="btn btn-icon"
          onClick={() => void reload()}
          disabled={loading}
          aria-label="Refresh"
          title="Refresh"
        >
          <svg
            viewBox="0 0 24 24"
            width="16"
            height="16"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className={loading ? "spin" : ""}
          >
            <polyline points="23 4 23 10 17 10" />
            <polyline points="1 20 1 14 7 14" />
            <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
          </svg>
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
              onView={(l) => openModal({ kind: "view", link: l })}
              onEdit={(l) => openModal({ kind: "edit", link: l })}
              onDelete={(l) => openModal({ kind: "delete", link: l })}
            />
          ))
        )}
      </div>

      {modal.kind === "create" && (
        <Modal title="Create link" onClose={closeModal}>
          <LinkForm
            mode="create"
            busy={busy}
            error={modalError}
            types={types}
            onSubmit={handleCreate}
            onCancel={closeModal}
          />
        </Modal>
      )}

      {modal.kind === "view" && (
        <Modal title="Link details" onClose={closeModal} wide>
          <LinkDetail
            link={modal.link}
            onClose={closeModal}
            onEdit={() => openModal({ kind: "edit", link: modal.link })}
            onDelete={() => openModal({ kind: "delete", link: modal.link })}
          />
        </Modal>
      )}

      {modal.kind === "edit" && (
        <Modal title="Edit link" onClose={closeModal}>
          <LinkForm
            mode="edit"
            busy={busy}
            error={modalError}
            types={types}
            initial={modal.link}
            onSubmit={handleEdit}
            onCancel={closeModal}
          />
        </Modal>
      )}

      {modal.kind === "delete" && (
        <Modal title="Delete link?" onClose={closeModal}>
          {modalError && <div className="form-error">{modalError}</div>}
          <p>
            Delete <code>{modal.link.short_id}</code>? This soft-deletes the link;
            it stays recoverable in the backing store for the grace window.
          </p>
          <div className="modal-actions">
            <button
              type="button"
              className="btn"
              onClick={closeModal}
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
    </main>
  );
}

function Modal({
  title,
  children,
  onClose,
  wide,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
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
      <div className={`modal ${wide ? "wide" : ""}`} onClick={(e) => e.stopPropagation()}>
        <h2>{title}</h2>
        {children}
      </div>
    </div>
  );
}
