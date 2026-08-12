import {lazy, Suspense, useEffect} from "react";
import {
    Navigate,
    Outlet,
    Route,
    Routes,
    useLocation,
} from "react-router-dom";

import {Footer} from "./components/Footer";
import {TopNav} from "./components/TopNav";
import {HomePage} from "./pages/HomePage";
import {NotFoundPage} from "./pages/NotFoundPage";

const CatalogPage = lazy(() => import("./pages/CatalogPage"));
const CoursePage = lazy(() => import("./pages/CoursePage"));
const SwapPage = lazy(() => import("./pages/SwapPage"));
const LearningPage = lazy(() => import("./pages/LearningPage"));
const AccountLayout = lazy(() => import("./pages/AccountLayout"));
const AccountOrdersPage = lazy(() => import("./pages/AccountLayout").then((module) => ({default: module.AccountOrdersPage})));
const AccountEnrollmentsPage = lazy(() => import("./pages/AccountLayout").then((module) => ({default: module.AccountEnrollmentsPage})));
const AccountCertificatesPage = lazy(() => import("./pages/AccountLayout").then((module) => ({default: module.AccountCertificatesPage})));
const AccountCommentsPage = lazy(() => import("./pages/AccountLayout").then((module) => ({default: module.AccountCommentsPage})));
const StudioPage = lazy(() => import("./pages/StudioPage"));
const AdminLayout = lazy(() => import("./features/admin/AdminLayout"));
const UsersPage = lazy(() => import("./features/admin/users/UsersPage"));
const CourseReviewPage = lazy(() => import("./features/admin/courses/CourseReviewPage"));
const ChainStatusPanel = lazy(() => import("./features/admin/chain/ChainStatusPanel"));
const DlqPage = lazy(() => import("./features/admin/dlq/DlqPage"));

function ScrollRestoration() {
    const {pathname} = useLocation();
    useEffect(() => {
        window.scrollTo({top: 0, behavior: "instant"});
        document.getElementById("main-content")?.focus({preventScroll: true});
    }, [pathname]);
    return null;
}

function AppShell() {
    return (
        <>
            <ScrollRestoration />
            <a className="skip-link" href="#main-content">跳到主内容</a>
            <TopNav />
            <main id="main-content" className="container app-main" tabIndex={-1}>
                <Suspense fallback={<div className="route-loader" role="status">页面加载中…</div>}>
                    <Outlet />
                </Suspense>
            </main>
            <Footer />
        </>
    );
}

export function App() {
    return (
        <Routes>
            <Route element={<AppShell />}>
                <Route index element={<HomePage />} />
                <Route path="courses" element={<CatalogPage />} />
                <Route path="courses/:courseId" element={<CoursePage />} />
                <Route path="swap" element={<SwapPage />} />
                <Route path="learn/:courseId" element={<LearningPage />} />
                <Route path="account" element={<AccountLayout />}>
                    <Route index element={<Navigate to="enrollments" replace />} />
                    <Route path="orders" element={<AccountOrdersPage />} />
                    <Route path="enrollments" element={<AccountEnrollmentsPage />} />
                    <Route path="certificates" element={<AccountCertificatesPage />} />
                    <Route path="comments" element={<AccountCommentsPage />} />
                </Route>
                <Route path="studio" element={<StudioPage />} />
                <Route path="admin" element={<AdminLayout />}>
                    <Route index element={<Navigate to="courses" replace />} />
                    <Route path="users" element={<UsersPage />} />
                    <Route path="courses" element={<CourseReviewPage />} />
                    <Route path="chain" element={<ChainStatusPanel />} />
                    <Route path="dlq" element={<DlqPage />} />
                </Route>
                <Route path="*" element={<NotFoundPage />} />
            </Route>
        </Routes>
    );
}
