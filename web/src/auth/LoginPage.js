import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { ArrowLeft, ArrowRight, Sparkles, Target, ShieldCheck, PhoneCall, Mail, User as UserIcon, Building2, KeyRound, CheckCircle2, LogIn, } from 'lucide-react';
import { useAuth } from './AuthContext';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { Spinner } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';
export function LoginPage() {
    const { signInWithPair } = useAuth();
    const navigate = useNavigate();
    const location = useLocation();
    const [mode, setMode] = useState('signin');
    const [step, setStep] = useState('email');
    const [email, setEmail] = useState('');
    const [name, setName] = useState('');
    const [tenant, setTenant] = useState('demo');
    const [code, setCode] = useState('');
    const [error, setError] = useState(null);
    const [busy, setBusy] = useState(false);
    const dest = location.state?.from?.pathname ?? '/';
    const captureError = (err, fallback) => {
        if (err instanceof ApiError)
            setError({ code: err.code, message: err.message });
        else
            setError({ code: 'unknown', message: fallback });
    };
    const switchMode = (next) => {
        if (next === mode)
            return;
        setMode(next);
        setError(null);
        setStep('email');
        setCode('');
    };
    const requestOTP = async (e) => {
        e.preventDefault();
        setError(null);
        setBusy(true);
        try {
            if (mode === 'signup') {
                await authApi.signupRequestOTP({ email, name, tenant_slug: tenant });
            }
            else {
                await authApi.signinRequestOTP({ email });
            }
            setStep('code');
        }
        catch (err) {
            captureError(err, 'request failed');
        }
        finally {
            setBusy(false);
        }
    };
    const verifyOTP = async (e) => {
        e.preventDefault();
        setError(null);
        setBusy(true);
        try {
            const pair = mode === 'signup'
                ? await authApi.signupVerifyOTP({ email, code })
                : await authApi.signinVerifyOTP({ email, code });
            signInWithPair(pair);
            navigate(dest, { replace: true });
        }
        catch (err) {
            captureError(err, 'verification failed');
        }
        finally {
            setBusy(false);
        }
    };
    return (_jsxs("div", { className: "min-h-screen grid lg:grid-cols-2 bg-line-soft", children: [_jsx(BrandPanel, {}), _jsx("main", { className: "flex items-center justify-center px-6 py-12 lg:px-12 bg-line-soft", children: _jsxs("div", { className: "w-full max-w-[420px]", children: [_jsx("header", { className: "lg:hidden flex justify-center mb-8", children: _jsx(BrandMark, { dark: true }) }), step === 'email' ? (_jsxs("div", { className: "text-center", children: [_jsx("h1", { className: "font-display text-[34px] leading-[1.1] font-bold text-ink mb-2", children: mode === 'signup' ? 'Create your account' : 'Welcome back' }), _jsx("p", { className: "text-sm text-ink-sub mb-8", children: mode === 'signup'
                                        ? 'Get your team running in under a minute — no password needed.'
                                        : 'Sign in to your recruiter workspace' }), _jsxs("form", { onSubmit: requestOTP, className: "space-y-4 text-left", children: [_jsx(Field, { label: "Email address", children: _jsx(IconInput, { icon: _jsx(Mail, { className: "w-4 h-4" }), value: email, onChange: (e) => setEmail(e.target.value), type: "email", required: true, placeholder: "you@company.com", autoFocus: true, autoComplete: "email" }) }), mode === 'signup' && (_jsxs(_Fragment, { children: [_jsx(Field, { label: "Full name", children: _jsx(IconInput, { icon: _jsx(UserIcon, { className: "w-4 h-4" }), value: name, onChange: (e) => setName(e.target.value), required: true, placeholder: "Alice Recruiter", autoComplete: "name" }) }), _jsx(Field, { label: "Workspace slug", hint: "Your tenant identifier \u2014 use 'demo' for local dev.", children: _jsx(IconInput, { icon: _jsx(Building2, { className: "w-4 h-4" }), value: tenant, onChange: (e) => setTenant(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '')), required: true, placeholder: "demo" }) })] })), error && error.code !== 'user_not_found' && !(error.code === 'invalid_credentials' && mode === 'signin') && (_jsx(ErrorBanner, { children: error.message })), _jsxs(PrimaryButton, { type: "submit", disabled: busy || !email.trim() || (mode === 'signup' && (!name.trim() || !tenant.trim())), children: [busy ? _jsx(Spinner, {}) : _jsx(LogIn, { className: "w-4 h-4" }), mode === 'signup' ? 'Create account' : 'Sign in'] }), _jsxs("div", { className: "text-center pt-1 space-y-1", children: [error && (error.code === 'user_not_found' || (error.code === 'invalid_credentials' && mode === 'signin')) && (_jsx("p", { className: "text-xs font-bold text-accent", children: error.code === 'user_not_found'
                                                        ? 'No account found for this email.'
                                                        : "Couldn't sign you in. Check your email." })), _jsxs("p", { className: "text-xs text-ink-sub", children: [mode === 'signup' ? 'Have an account already? ' : 'New to hireflow? ', _jsx("button", { type: "button", onClick: () => switchMode(mode === 'signup' ? 'signin' : 'signup'), className: "text-accent font-bold hover:underline", children: mode === 'signup' ? 'Sign in instead' : 'Create one' })] })] })] })] })) : (_jsxs("div", { className: "text-center", children: [_jsxs("div", { className: "inline-flex items-center gap-2 px-2.5 py-1 rounded-full bg-accent-soft text-accent mb-4", children: [_jsx(KeyRound, { className: "w-3 h-3" }), _jsx("span", { className: "text-[10px] font-bold uppercase tracking-wider", children: "Verify it's you" })] }), _jsx("h1", { className: "font-display text-[34px] leading-[1.1] font-bold text-ink mb-2", children: "Enter your code" }), _jsxs("p", { className: "text-sm text-ink-sub mb-8", children: ["We sent a 6-digit code to ", _jsx("span", { className: "font-semibold text-ink", children: email }), ". It expires in 10 minutes."] }), _jsxs("form", { onSubmit: verifyOTP, className: "space-y-4 text-left", children: [_jsx(Field, { label: "6-digit code", children: _jsx("input", { value: code, onChange: (e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6)), inputMode: "numeric", pattern: "[0-9]{6}", required: true, autoFocus: true, autoComplete: "one-time-code", placeholder: "\u2022 \u2022 \u2022 \u2022 \u2022 \u2022", className: "w-full h-14 px-3 rounded-[10px] bg-white border border-line focus:outline-none focus:border-accent font-mono text-2xl tracking-[0.5em] text-center text-ink placeholder:text-ink-mute placeholder:tracking-[0.4em] transition-colors shadow-sm" }) }), error && _jsx(ErrorBanner, { children: error.message }), _jsxs(PrimaryButton, { type: "submit", disabled: busy || code.length !== 6, children: [busy ? _jsx(Spinner, {}) : _jsx(CheckCircle2, { className: "w-4 h-4" }), "Verify and continue", !busy && _jsx(ArrowRight, { className: "w-4 h-4" })] }), _jsx("p", { className: "text-center pt-1", children: _jsxs("button", { type: "button", onClick: () => { setStep('email'); setCode(''); setError(null); }, className: "inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink", children: [_jsx(ArrowLeft, { className: "w-3 h-3" }), " Use a different email"] }) })] })] }))] }) })] }));
}
function BrandPanel() {
    return (_jsxs("aside", { className: "hidden lg:flex flex-col justify-between p-12 xl:p-16 relative overflow-hidden text-white", style: {
            background: 'radial-gradient(900px 600px at 110% -10%, rgba(255,255,255,0.08), transparent 60%),' +
                'radial-gradient(900px 600px at -10% 110%, rgba(0,0,0,0.18), transparent 60%),' +
                'linear-gradient(160deg, #6E5BFF 0%, #5B4CFF 45%, #4A3DE6 100%)',
        }, children: [_jsx("div", { className: "absolute inset-0 pointer-events-none opacity-[0.18]", style: {
                    backgroundImage: 'radial-gradient(circle at 1px 1px, rgba(255,255,255,0.6) 1px, transparent 0)',
                    backgroundSize: '22px 22px',
                } }), _jsx("header", { className: "relative z-10", children: _jsx(BrandMark, {}) }), _jsxs("div", { className: "relative z-10 max-w-[480px]", children: [_jsxs("h2", { className: "font-display text-[52px] xl:text-[60px] leading-[1.02] font-bold mb-5", children: ["AI-Driven", _jsx("br", {}), "Recruiter", _jsx("br", {}), "Workspace"] }), _jsx("p", { className: "text-base text-white/75 leading-relaxed max-w-[420px]", children: "Capture hiring intent in plain English, draft postings, source candidates, and verify trust \u2014 end to end, in one place." })] }), _jsxs("ul", { className: "relative z-10 space-y-3.5", children: [_jsx(Feature, { icon: _jsx(Target, { className: "w-3.5 h-3.5" }), label: "Conversational hiring-intent capture" }), _jsx(Feature, { icon: _jsx(ShieldCheck, { className: "w-3.5 h-3.5" }), label: "Trust-first ID, liveness & background checks" }), _jsx(Feature, { icon: _jsx(PhoneCall, { className: "w-3.5 h-3.5" }), label: "Autonomous AI voice screening calls" })] })] }));
}
function BrandMark({ dark = false }) {
    return (_jsxs("div", { className: "inline-flex items-center gap-2.5", children: [_jsx("div", { className: cn('w-9 h-9 rounded-[10px] flex items-center justify-center shadow-sm', dark ? 'bg-accent text-white' : 'bg-white/95 text-accent'), children: _jsx(Sparkles, { className: "w-4.5 h-4.5" }) }), _jsx("div", { className: cn('font-display text-xl font-bold', dark ? 'text-ink' : 'text-white'), children: "hireflow" })] }));
}
function Feature({ icon, label }) {
    return (_jsxs("li", { className: "flex items-center gap-3", children: [_jsx("div", { className: "flex-shrink-0 w-8 h-8 rounded-full bg-white/12 backdrop-blur flex items-center justify-center text-white", children: icon }), _jsx("span", { className: "text-sm text-white/90", children: label })] }));
}
function Field({ label, hint, children }) {
    return (_jsxs("div", { children: [_jsx("label", { className: "block text-[10px] font-bold uppercase tracking-wider text-ink-sub mb-1.5", children: label }), children, hint && _jsx("p", { className: "mt-1 text-[11px] text-ink-mute", children: hint })] }));
}
function IconInput({ icon, ...props }) {
    return (_jsxs("div", { className: "relative", children: [_jsx("span", { className: "absolute left-3 top-1/2 -translate-y-1/2 text-ink-mute pointer-events-none", children: icon }), _jsx("input", { ...props, className: cn('w-full h-11 pl-10 pr-3.5 rounded-[10px] text-sm bg-white border border-line text-ink placeholder:text-ink-mute focus:outline-none focus:border-accent shadow-sm transition-colors', props.className) })] }));
}
function PrimaryButton({ children, ...props }) {
    return (_jsx("button", { ...props, className: cn('w-full h-11 inline-flex items-center justify-center gap-2 rounded-[10px] bg-accent hover:bg-accent-hover text-white text-sm font-bold transition-colors disabled:opacity-40 disabled:cursor-not-allowed shadow-sm', props.className), children: children }));
}
function ErrorBanner({ children }) {
    return (_jsx("div", { className: "px-3 py-2 rounded-[10px] bg-red-50 border border-red-100 text-[12px] text-red-700", children: children }));
}
