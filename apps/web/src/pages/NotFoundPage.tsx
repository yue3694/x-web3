import {Link} from "react-router-dom";

export function NotFoundPage() {
    return <div className="not-found panel"><span className="eyebrow">404 · Route not found</span><h1>This page is offchain.</h1><p>The address does not map to a product route.</p><Link className="btn btn--primary" to="/">Return home</Link></div>;
}
