import {TopNav} from "./components/TopNav";
import {Hero} from "./components/Hero";
import {Footer} from "./components/Footer";
import {CourseCatalog} from "./features/catalog/CourseCatalog";
import {CourseEditor} from "./features/teacher/CourseEditor";
import {RequirePermission} from "./auth/RequirePermission";

/**
 * 主页布局：
 *   TopNav (sticky)
 *     ↓
 *   Hero — 品牌主视觉 + 统计
 *     ↓
 *   Catalog — 公开课程
 *     ↓
 *   Teacher Studio — 需 COURSE_CREATE
 *     ↓
 *   Admin — 需 SYSTEM_ADMIN
 *     ↓
 *   Footer — 多列导航 + 状态条
 *
 * 历史 wallet/account 信息现在统一在 TopNav + UserMenu 下拉里展示，
 * 不再在主区域里重复（修复 AccountMenu 内容重复问题）。
 * Notepad 已移除 — 该功能迁出主页，未来作为独立 feature。
 */
export function App() {
    return (
        <>
            <TopNav />

            <main className="container">
                <Hero />

                <CourseCatalog />

                <RequirePermission code="COURSE_CREATE">
                    <div id="studio" />
                    <CourseEditor />
                </RequirePermission>

                <RequirePermission code="SYSTEM_ADMIN">
                    <div id="admin" className="admin-anchor" />
                </RequirePermission>
            </main>

            <Footer />
        </>
    );
}