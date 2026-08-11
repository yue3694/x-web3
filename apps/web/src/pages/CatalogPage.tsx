import {CourseCatalog} from "@/features/catalog/CourseCatalog";
import {TARGET_CHAIN_NAME} from "@/chains";

export default function CatalogPage() {
    return (
        <div className="page-stack">
            <header className="page-hero">
                <span className="eyebrow">Course marketplace</span>
                <h1>Find your next onchain skill.</h1>
                <p>Search published courses, inspect the complete curriculum, then enroll through a verified {TARGET_CHAIN_NAME} transaction.</p>
            </header>
            <CourseCatalog />
        </div>
    );
}
