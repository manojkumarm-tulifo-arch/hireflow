import { useState, type FormEvent, type ReactNode } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import {
  ArrowLeft, ArrowRight, Sparkles, Target, ShieldCheck, PhoneCall,
  Mail, User as UserIcon, Building2, KeyRound, CheckCircle2, LogIn,
} from 'lucide-react';
import { useAuth } from './AuthContext';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { Spinner } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';

interface LocationState { from?: { pathname: string } }

type Mode = 'signin' | 'signup';
type Step = 'email' | 'code';

export function LoginPage() {
  const { signInWithPair } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  const [mode, setMode] = useState<Mode>('signin');
  const [step, setStep] = useState<Step>('email');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [tenant, setTenant] = useState('demo');
  const [code, setCode] = useState('');
  const [error, setError] = useState<{ code: string; message: string } | null>(null);
  const [busy, setBusy] = useState(false);

  const dest = (location.state as LocationState | null)?.from?.pathname ?? '/';

  const captureError = (err: unknown, fallback: string) => {
    if (err instanceof ApiError) setError({ code: err.code, message: err.message });
    else setError({ code: 'unknown', message: fallback });
  };

  const switchMode = (next: Mode) => {
    if (next === mode) return;
    setMode(next); setError(null); setStep('email'); setCode('');
  };

  const requestOTP = async (e: FormEvent) => {
    e.preventDefault();
    setError(null); setBusy(true);
    try {
      if (mode === 'signup') {
        await authApi.signupRequestOTP({ email, name, tenant_slug: tenant });
      } else {
        await authApi.signinRequestOTP({ email });
      }
      setStep('code');
    } catch (err) {
      captureError(err, 'request failed');
    } finally {
      setBusy(false);
    }
  };

  const verifyOTP = async (e: FormEvent) => {
    e.preventDefault();
    setError(null); setBusy(true);
    try {
      const pair = mode === 'signup'
        ? await authApi.signupVerifyOTP({ email, code })
        : await authApi.signinVerifyOTP({ email, code });
      signInWithPair(pair);
      navigate(dest, { replace: true });
    } catch (err) {
      captureError(err, 'verification failed');
    } finally {
      setBusy(false);
    }
  };

  // Codes that mean "this OTP can no longer succeed; user needs a new one".
  // We surface a one-click "Send a fresh code" CTA alongside the banner.
  const otpUnusable = error?.code === 'otp_expired'
    || error?.code === 'otp_max_attempts'
    || error?.code === 'otp_already_used';

  const resendOTP = async () => {
    setError(null); setCode(''); setBusy(true);
    try {
      if (mode === 'signup') {
        await authApi.signupRequestOTP({ email, name, tenant_slug: tenant });
      } else {
        await authApi.signinRequestOTP({ email });
      }
      // Stay on the code step; the input is now waiting for the new code.
    } catch (err) {
      captureError(err, 'resend failed');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="min-h-screen grid lg:grid-cols-2 bg-line-soft">
      <BrandPanel />

      <main className="flex items-center justify-center px-6 py-12 lg:px-12 bg-line-soft">
        <div className="w-full max-w-[420px]">
          <header className="lg:hidden flex justify-center mb-8">
            <BrandMark dark />
          </header>

          {step === 'email' ? (
            <div className="text-center">
              <h1 className="font-display text-[34px] leading-[1.1] font-bold text-ink mb-2">
                {mode === 'signup' ? 'Create your account' : 'Welcome back'}
              </h1>
              <p className="text-sm text-ink-sub mb-8">
                {mode === 'signup'
                  ? 'Get your team running in under a minute — no password needed.'
                  : 'Sign in to your recruiter workspace'}
              </p>

              <form onSubmit={requestOTP} className="space-y-4 text-left">
                <Field label="Email address">
                  <IconInput
                    icon={<Mail className="w-4 h-4" />}
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    type="email"
                    required
                    placeholder="you@company.com"
                    autoFocus
                    autoComplete="email"
                  />
                </Field>

                {mode === 'signup' && (
                  <>
                    <Field label="Full name">
                      <IconInput
                        icon={<UserIcon className="w-4 h-4" />}
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        required
                        placeholder="Alice Recruiter"
                        autoComplete="name"
                      />
                    </Field>
                    <Field label="Workspace slug" hint="Your tenant identifier — use 'demo' for local dev.">
                      <IconInput
                        icon={<Building2 className="w-4 h-4" />}
                        value={tenant}
                        onChange={(e) => setTenant(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
                        required
                        placeholder="demo"
                      />
                    </Field>
                  </>
                )}

                {error && error.code !== 'user_not_found' && !(error.code === 'invalid_credentials' && mode === 'signin') && (
                  <ErrorBanner>{error.message}</ErrorBanner>
                )}

                <PrimaryButton type="submit" disabled={busy || !email.trim() || (mode === 'signup' && (!name.trim() || !tenant.trim()))}>
                  {busy ? <Spinner /> : <LogIn className="w-4 h-4" />}
                  {mode === 'signup' ? 'Create account' : 'Sign in'}
                </PrimaryButton>

                <div className="text-center pt-1 space-y-1">
                  {error && (error.code === 'user_not_found' || (error.code === 'invalid_credentials' && mode === 'signin')) && (
                    <p className="text-xs font-bold text-accent">
                      {error.code === 'user_not_found'
                        ? 'No account found for this email.'
                        : "Couldn't sign you in. Check your email."}
                    </p>
                  )}
                  <p className="text-xs text-ink-sub">
                    {mode === 'signup' ? 'Have an account already? ' : 'New to hireflow? '}
                    <button
                      type="button"
                      onClick={() => switchMode(mode === 'signup' ? 'signin' : 'signup')}
                      className="text-accent font-bold hover:underline"
                    >
                      {mode === 'signup' ? 'Sign in instead' : 'Create one'}
                    </button>
                  </p>
                </div>
              </form>
            </div>
          ) : (
            <div className="text-center">
              <div className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full bg-accent-soft text-accent mb-4">
                <KeyRound className="w-3 h-3" />
                <span className="text-[10px] font-bold uppercase tracking-wider">Verify it's you</span>
              </div>
              <h1 className="font-display text-[34px] leading-[1.1] font-bold text-ink mb-2">
                Enter your code
              </h1>
              <p className="text-sm text-ink-sub mb-8">
                We sent a 6-digit code to <span className="font-semibold text-ink">{email}</span>. It expires in 10 minutes.
              </p>

              <form onSubmit={verifyOTP} className="space-y-4 text-left">
                <Field label="6-digit code">
                  <input
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    inputMode="numeric"
                    pattern="[0-9]{6}"
                    required
                    autoFocus
                    autoComplete="one-time-code"
                    placeholder="• • • • • •"
                    className="w-full h-14 px-3 rounded-[10px] bg-white border border-line focus:outline-none focus:border-accent font-mono text-2xl tracking-[0.5em] text-center text-ink placeholder:text-ink-mute placeholder:tracking-[0.4em] transition-colors shadow-sm"
                  />
                </Field>

                {error && (
                  <div className="space-y-2">
                    <ErrorBanner>{error.message}</ErrorBanner>
                    {otpUnusable && (
                      <button
                        type="button"
                        onClick={resendOTP}
                        disabled={busy}
                        className="text-xs font-bold text-accent hover:underline disabled:opacity-40"
                      >
                        Send a fresh code →
                      </button>
                    )}
                  </div>
                )}

                <PrimaryButton type="submit" disabled={busy || code.length !== 6 || otpUnusable}>
                  {busy ? <Spinner /> : <CheckCircle2 className="w-4 h-4" />}
                  Verify and continue
                  {!busy && <ArrowRight className="w-4 h-4" />}
                </PrimaryButton>

                <p className="text-center pt-1">
                  <button
                    type="button"
                    onClick={() => { setStep('email'); setCode(''); setError(null); }}
                    className="inline-flex items-center gap-1 text-xs text-ink-sub hover:text-ink"
                  >
                    <ArrowLeft className="w-3 h-3" /> Use a different email
                  </button>
                </p>
              </form>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function BrandPanel() {
  return (
    <aside
      className="hidden lg:flex flex-col justify-between p-12 xl:p-16 relative overflow-hidden text-white"
      style={{
        background:
          'radial-gradient(900px 600px at 110% -10%, rgba(255,255,255,0.08), transparent 60%),' +
          'radial-gradient(900px 600px at -10% 110%, rgba(0,0,0,0.18), transparent 60%),' +
          'linear-gradient(160deg, #6E5BFF 0%, #5B4CFF 45%, #4A3DE6 100%)',
      }}
    >
      <div
        className="absolute inset-0 pointer-events-none opacity-[0.18]"
        style={{
          backgroundImage:
            'radial-gradient(circle at 1px 1px, rgba(255,255,255,0.6) 1px, transparent 0)',
          backgroundSize: '22px 22px',
        }}
      />

      <header className="relative z-10">
        <BrandMark />
      </header>

      <div className="relative z-10 max-w-[480px]">
        <h2 className="font-display text-[52px] xl:text-[60px] leading-[1.02] font-bold mb-5">
          AI-Driven<br />Recruiter<br />Workspace
        </h2>
        <p className="text-base text-white/75 leading-relaxed max-w-[420px]">
          Capture hiring intent in plain English, draft postings, source candidates, and verify trust — end to end, in one place.
        </p>
      </div>

      <ul className="relative z-10 space-y-3.5">
        <Feature icon={<Target className="w-3.5 h-3.5" />} label="Conversational hiring-intent capture" />
        <Feature icon={<ShieldCheck className="w-3.5 h-3.5" />} label="Trust-first ID, liveness & background checks" />
        <Feature icon={<PhoneCall className="w-3.5 h-3.5" />} label="Autonomous AI voice screening calls" />
      </ul>
    </aside>
  );
}

function BrandMark({ dark = false }: { dark?: boolean }) {
  return (
    <div className="inline-flex items-center gap-2.5">
      <div
        className={cn(
          'w-9 h-9 rounded-[10px] flex items-center justify-center shadow-sm',
          dark ? 'bg-accent text-white' : 'bg-white/95 text-accent',
        )}
      >
        <Sparkles className="w-4.5 h-4.5" />
      </div>
      <div className={cn('font-display text-xl font-bold', dark ? 'text-ink' : 'text-white')}>hireflow</div>
    </div>
  );
}

function Feature({ icon, label }: { icon: ReactNode; label: string }) {
  return (
    <li className="flex items-center gap-3">
      <div className="flex-shrink-0 w-8 h-8 rounded-full bg-white/12 backdrop-blur flex items-center justify-center text-white">
        {icon}
      </div>
      <span className="text-sm text-white/90">{label}</span>
    </li>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div>
      <label className="block text-[10px] font-bold uppercase tracking-wider text-ink-sub mb-1.5">
        {label}
      </label>
      {children}
      {hint && <p className="mt-1 text-[11px] text-ink-mute">{hint}</p>}
    </div>
  );
}

function IconInput({ icon, ...props }: React.InputHTMLAttributes<HTMLInputElement> & { icon: ReactNode }) {
  return (
    <div className="relative">
      <span className="absolute left-3 top-1/2 -translate-y-1/2 text-ink-mute pointer-events-none">{icon}</span>
      <input
        {...props}
        className={cn(
          'w-full h-11 pl-10 pr-3.5 rounded-[10px] text-sm bg-white border border-line text-ink placeholder:text-ink-mute focus:outline-none focus:border-accent shadow-sm transition-colors',
          props.className,
        )}
      />
    </div>
  );
}

function PrimaryButton({ children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...props}
      className={cn(
        'w-full h-11 inline-flex items-center justify-center gap-2 rounded-[10px] bg-accent hover:bg-accent-hover text-white text-sm font-bold transition-colors disabled:opacity-40 disabled:cursor-not-allowed shadow-sm',
        props.className,
      )}
    >
      {children}
    </button>
  );
}

function ErrorBanner({ children }: { children: ReactNode }) {
  return (
    <div className="px-3 py-2 rounded-[10px] bg-red-50 border border-red-100 text-[12px] text-red-700">
      {children}
    </div>
  );
}


