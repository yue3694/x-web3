import {useEffect, useMemo, useState} from "react";
import type {ReactNode} from "react";
import {
    useAccount,
    useReadContract,
    useWaitForTransactionReceipt,
    useWriteContract,
} from "wagmi";

import {notepadAbi} from "../contracts/notepad.abi";
import {notepadDeployments} from "../contracts/deployments";

const TITLE_MAX = 64;
const BODY_MAX = 1024;
const MAX_NOTES = 50;

type Mode = "view" | "create" | "edit";

type Note = {
    id: bigint;
    title: string;
    body: string;
    createdAt: bigint;
    updatedAt: bigint;
};

export function Notepad(): ReactNode {
    const {isConnected, chainId, address} = useAccount();

    const deployment = notepadDeployments.sepolia;
    const hasAddress = Boolean(deployment.address);
    const isOnSepolia = chainId === deployment.chainId;
    const ready = hasAddress && isConnected && isOnSepolia;

    // ---------- reads ----------
    const {
        data: notesRaw,
        refetch,
        isFetching,
    } = useReadContract({
        abi: notepadAbi,
        address: deployment.address,
        functionName: "getNotes",
        args: address ? [address] : undefined,
        chainId: deployment.chainId,
        query: {enabled: ready && Boolean(address)},
    });

    const notes = useMemo<Note[]>(() => {
        if (!Array.isArray(notesRaw)) return [];
        // Sort by id ascending so swap-and-pop deletion order doesn't confuse the user.
        return (notesRaw as Note[])
            .slice()
            .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
    }, [notesRaw]);

    // ---------- writes ----------
    const {data: hash, writeContract, isPending, error, reset} =
        useWriteContract();
    const {isLoading: isConfirming, isSuccess: isConfirmed} =
        useWaitForTransactionReceipt({hash});

    // ---------- editor state ----------
    const [mode, setMode] = useState<Mode>("view");
    const [selectedId, setSelectedId] = useState<bigint | null>(null);
    const [draftTitle, setDraftTitle] = useState("");
    const [draftBody, setDraftBody] = useState("");

    // After a successful tx, refresh notes and reset the editor.
    useEffect(() => {
        if (!isConfirmed) return;
        refetch();
        setMode("view");
        setSelectedId(null);
        setDraftTitle("");
        setDraftBody("");
        reset();
    }, [isConfirmed, refetch, reset]);

    const selectedNote = useMemo(
        () => notes.find((n) => n.id === selectedId) ?? null,
        [notes, selectedId],
    );

    // ---------- handlers ----------
    const openNew = () => {
        setMode("create");
        setSelectedId(null);
        setDraftTitle("");
        setDraftBody("");
    };

    const openView = (note: Note) => {
        setMode("view");
        setSelectedId(note.id);
        setDraftTitle(note.title);
        setDraftBody(note.body);
    };

    const cancel = () => {
        setMode("view");
        setSelectedId(null);
        setDraftTitle("");
        setDraftBody("");
    };

    const beginEdit = () => {
        if (!selectedNote) return;
        setMode("edit");
    };

    const submit = () => {
        if (!deployment.address || !address) return;
        if (mode === "create") {
            writeContract({
                abi: notepadAbi,
                address: deployment.address,
                functionName: "createNote",
                args: [draftTitle, draftBody],
                chainId: deployment.chainId,
            });
        } else if (mode === "edit" && selectedId !== null) {
            writeContract({
                abi: notepadAbi,
                address: deployment.address,
                functionName: "updateNote",
                args: [selectedId, draftTitle, draftBody],
                chainId: deployment.chainId,
            });
        }
    };

    const remove = () => {
        if (!deployment.address || selectedId === null) return;
        writeContract({
            abi: notepadAbi,
            address: deployment.address,
            functionName: "deleteNote",
            args: [selectedId],
            chainId: deployment.chainId,
        });
    };

    // ---------- guards ----------
    if (!hasAddress) {
        return (
            <article className="card">
                <h2>Notepad</h2>
                <p className="muted">
                    No deployment address found. Run
                    <code> pnpm contracts:deploy:notepad:sepolia </code>
                    and paste the printed address into
                    <code>
                        {" "}
                        apps/web/src/contracts/deployments.ts
                    </code>
                    .
                </p>
            </article>
        );
    }
    if (!isConnected) {
        return (
            <article className="card">
                <h2>Notepad</h2>
                <p className="muted">Connect a wallet to use your notepad.</p>
            </article>
        );
    }
    if (!isOnSepolia) {
        return (
            <article className="card">
                <h2>Notepad</h2>
                <p className="warn">
                    Wrong network. Switch to <strong>Sepolia</strong> in your
                    wallet.
                </p>
            </article>
        );
    }

    const titleBytes = new TextEncoder().encode(draftTitle).length;
    const bodyBytes = new TextEncoder().encode(draftBody).length;
    const titleOver = titleBytes > TITLE_MAX;
    const bodyOver = bodyBytes > BODY_MAX;
    const dirty =
        mode === "create"
            ? draftTitle !== "" || draftBody !== ""
            : mode === "edit" && selectedNote !== null
              ? draftTitle !== selectedNote.title ||
                draftBody !== selectedNote.body
              : false;

    const busy = isPending || isConfirming;
    const canSave =
        !busy &&
        (mode === "create" || mode === "edit") &&
        !titleOver &&
        !bodyOver &&
        dirty;

    return (
        <article className="notepad">
            <aside className="notes-list">
                <header className="notes-list__header">
                    <h2>Notes ({notes.length}/{MAX_NOTES})</h2>
                    <button
                        type="button"
                        disabled={busy || notes.length >= MAX_NOTES}
                        onClick={openNew}
                    >
                        + New
                    </button>
                </header>
                {isFetching && notes.length === 0 ? (
                    <p className="muted">Loading…</p>
                ) : notes.length === 0 ? (
                    <p className="muted">
                        No notes yet. Click <strong>+ New</strong> to start.
                    </p>
                ) : (
                    <ul className="notes-list__items">
                        {notes.map((note) => (
                            <li
                                key={note.id.toString()}
                                className={
                                    selectedId === note.id
                                        ? "notes-list__item notes-list__item--active"
                                        : "notes-list__item"
                                }
                            >
                                <button
                                    type="button"
                                    className="notes-list__btn"
                                    disabled={busy}
                                    onClick={() => openView(note)}
                                >
                                    <span className="notes-list__title">
                                        #{note.id.toString()}{" "}
                                        {note.title || "(untitled)"}
                                    </span>
                                    <span className="notes-list__meta">
                                        {formatRelative(Number(note.updatedAt))}
                                    </span>
                                </button>
                            </li>
                        ))}
                    </ul>
                )}
            </aside>

            <section className="editor">
                {mode === "view" && selectedNote === null && (
                    <div className="editor__empty">
                        <h2>Editor</h2>
                        <p className="muted">
                            Select a note on the left, or click <strong>+ New</strong>.
                        </p>
                    </div>
                )}

                {mode === "view" && selectedNote !== null && (
                    <div className="editor__view">
                        <header className="editor__header">
                            <h2>{selectedNote.title || "(untitled)"}</h2>
                            <div className="editor__actions">
                                <button
                                    type="button"
                                    disabled={busy}
                                    onClick={beginEdit}
                                >
                                    Edit
                                </button>
                                <button
                                    type="button"
                                    className="danger"
                                    disabled={busy}
                                    onClick={remove}
                                >
                                    Delete
                                </button>
                            </div>
                        </header>
                        <p className="muted">
                            Created {formatAbsolute(Number(selectedNote.createdAt))} ·{" "}
                            Updated {formatAbsolute(Number(selectedNote.updatedAt))}
                        </p>
                        <pre className="editor__body">
                            {selectedNote.body || "(empty)"}
                        </pre>
                    </div>
                )}

                {(mode === "create" || mode === "edit") && (
                    <form
                        className="editor__form"
                        onSubmit={(e) => {
                            e.preventDefault();
                            submit();
                        }}
                    >
                        <header className="editor__header">
                            <h2>{mode === "create" ? "New note" : "Edit note"}</h2>
                        </header>

                        <label className="editor__label">
                            Title
                            <input
                                type="text"
                                value={draftTitle}
                                maxLength={TITLE_MAX}
                                disabled={busy}
                                onChange={(e) =>
                                    setDraftTitle(e.target.value)
                                }
                                placeholder="Headline"
                            />
                            <span
                                className={
                                    titleOver
                                        ? "editor__counter editor__counter--over"
                                        : "editor__counter"
                                }
                            >
                                {titleBytes}/{TITLE_MAX}
                            </span>
                        </label>

                        <label className="editor__label">
                            Body
                            <textarea
                                value={draftBody}
                                maxLength={BODY_MAX}
                                rows={10}
                                disabled={busy}
                                onChange={(e) =>
                                    setDraftBody(e.target.value)
                                }
                                placeholder="Write your note…"
                            />
                            <span
                                className={
                                    bodyOver
                                        ? "editor__counter editor__counter--over"
                                        : "editor__counter"
                                }
                            >
                                {bodyBytes}/{BODY_MAX}
                            </span>
                        </label>

                        <div className="editor__actions">
                            <button
                                type="submit"
                                disabled={!canSave}
                            >
                                {busy
                                    ? isConfirming
                                        ? "Mining…"
                                        : "Confirm in wallet…"
                                    : "Save"}
                            </button>
                            <button
                                type="button"
                                disabled={busy}
                                onClick={cancel}
                            >
                                Cancel
                            </button>
                        </div>
                    </form>
                )}

                {hash && (
                    <p className="muted editor__tx">
                        Tx:{" "}
                        <a
                            href={`https://sepolia.etherscan.io/tx/${hash}`}
                            target="_blank"
                            rel="noreferrer"
                        >
                            {hash.slice(0, 10)}…
                        </a>{" "}
                        {isConfirming && "(mining)"}
                        {isConfirmed && "(✓)"}
                    </p>
                )}
                {error && <p className="error">{error.message}</p>}
            </section>
        </article>
    );
}

// ---------- formatting helpers ----------
function formatRelative(tsSec: number): string {
    if (!tsSec) return "—";
    const diffSec = Math.floor(Date.now() / 1000 - tsSec);
    if (diffSec < 60) return "just now";
    if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
    const days = Math.floor(diffSec / 86400);
    if (days < 30) return `${days}d ago`;
    return new Date(tsSec * 1000).toLocaleDateString();
}

function formatAbsolute(tsSec: number): string {
    if (!tsSec) return "—";
    return new Date(tsSec * 1000).toLocaleString();
}