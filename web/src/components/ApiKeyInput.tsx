import { useEffect, useState } from "react";
import { getApiKey, setApiKey } from "../api";

export function ApiKeyInput() {
  const [value, setValue] = useState("");
  const [stored, setStored] = useState("");
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    const existing = getApiKey();
    setStored(existing);
    setEditing(!existing);
  }, []);

  const onSave = () => {
    setApiKey(value);
    setStored(value);
    setValue("");
    setEditing(false);
  };

  const onClear = () => {
    setApiKey("");
    setStored("");
    setValue("");
    setEditing(true);
  };

  return (
    <div className="api-key" title={stored ? "API key set" : "No API key"}>
      <span className={`status-dot ${stored ? "active" : ""}`} />
      {editing ? (
        <>
          <input
            type="password"
            placeholder="API key"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") onSave();
            }}
          />
          <button className="btn btn-ghost" onClick={onSave} disabled={!value}>
            Save
          </button>
        </>
      ) : (
        <>
          <input type="password" readOnly value="••••••••••" />
          <button className="btn btn-ghost" onClick={onClear}>
            Clear
          </button>
        </>
      )}
    </div>
  );
}
