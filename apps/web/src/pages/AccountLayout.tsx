import {NavLink, Outlet} from "react-router-dom";

import {RequireAuth} from "@/auth/RequireAuth";
import {SignInButton} from "@/auth/SignInButton";
import {MyCertificates} from "@/features/account/MyCertificates";
import {MyComments} from "@/features/account/MyComments";
import {MyEnrollments} from "@/features/account/MyEnrollments";
import {MyOrders} from "@/features/account/MyOrders";

const TABS = [
    ["/account/enrollments", "Learning"],
    ["/account/orders", "Orders"],
    ["/account/certificates", "Certificates"],
    ["/account/comments", "Reviews"],
] as const;

function AccountLayout() {
    return (
        <RequireAuth fallback={<div className="auth-gate panel"><span className="eyebrow">Private workspace</span><h1>Sign in to open your account.</h1><p>Your courses, receipts, credentials and reviews live here.</p><SignInButton className="btn btn--primary">Sign in</SignInButton></div>}>
            <div className="page-stack">
                <header className="page-hero page-hero--compact">
                    <span className="eyebrow">Student workspace</span>
                    <h1>Your learning, clearly organized.</h1>
                    <p>Move between active courses, purchase history and verified achievements without losing context.</p>
                </header>
                <nav className="subnav" aria-label="Account sections">
                    {TABS.map(([to, label]) => <NavLink key={to} to={to} className={({isActive}) => `subnav__link${isActive ? " is-active" : ""}`}>{label}</NavLink>)}
                </nav>
                <Outlet />
            </div>
        </RequireAuth>
    );
}

export const AccountOrdersPage = MyOrders;
export const AccountEnrollmentsPage = MyEnrollments;
export const AccountCertificatesPage = MyCertificates;
export const AccountCommentsPage = MyComments;

export default AccountLayout;
