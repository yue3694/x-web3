import {useState, type FormEvent} from "react";
import {ApiClientError} from "../../api/client";
import {courseApi, type Course} from "../../api/types";

export function CourseEditor() {
    const [title, setTitle] = useState("");
    const [description, setDescription] = useState("");
    const [price, setPrice] = useState("0");
    const [course, setCourse] = useState<Course | null>(null);
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState("");

    async function create(event: FormEvent) {
        event.preventDefault();
        setBusy(true); setMessage("");
        try {
            const slug = `${title.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}-${Date.now().toString(36)}`;
            const created = await courseApi.create({slug, title, description, priceMinor: Math.round(Number(price) * 100), currency: "USD"});
            setCourse(created); setMessage("Draft saved. Add curriculum before submitting for review.");
        } catch (cause) {
            setMessage(cause instanceof ApiClientError ? cause.message : "Could not create the draft.");
        } finally { setBusy(false); }
    }

    async function submit() {
        if (!course) return;
        setBusy(true); setMessage("");
        try { const updated = await courseApi.submit(course.id); setCourse(updated); setMessage("Course submitted for admin review."); }
        catch (cause) { setMessage(cause instanceof ApiClientError ? cause.message : "Could not submit the course."); }
        finally { setBusy(false); }
    }

    return (
        <section className="teacher-studio panel" aria-labelledby="studio-title">
            <div className="section-heading"><div><span className="eyebrow">Teacher workspace</span><h2 id="studio-title">Course studio</h2><p>Build a versioned course draft and send it through onchain university review.</p></div>{course ? <span className={`status-pill status-pill--${course.status}`}>{course.status.replace("_", " ")}</span> : null}</div>
            <form className="editor-grid card" onSubmit={create}>
                <label><span>Course title</span><input required maxLength={160} value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Smart Contract Security" /></label>
                <label><span>Price (USD)</span><input required min="0" step="0.01" type="number" value={price} onChange={(event) => setPrice(event.target.value)} /></label>
                <label className="editor-grid__wide"><span>Description</span><textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder="What will students be able to build?" rows={5} /></label>
                <div className="editor-actions editor-grid__wide"><button className="btn--primary" disabled={busy || course?.status === "pending_review"} type="submit">{busy ? "Saving..." : course ? "Create another draft" : "Create draft"}</button>{course?.status === "draft" ? <button className="btn--ghost" disabled={busy} onClick={() => void submit()} type="button">Submit review</button> : null}<span>{message}</span></div>
            </form>
        </section>
    );
}
