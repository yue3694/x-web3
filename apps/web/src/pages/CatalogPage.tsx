import {CourseCatalog} from "@/features/catalog/CourseCatalog";

export default function CatalogPage() {
    return (
        <div className="page-stack">
            <header className="page-hero">
                <span className="eyebrow">Course marketplace</span>
                <h1>Find your next onchain skill.</h1>
                <p>Search published courses, inspect the complete curriculum, then enroll through a verified Sepolia transaction.</p>
            </header>
            <CourseCatalog />
        </div>
    );
}
