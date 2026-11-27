import React, { useRef, useState } from "react"
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { HugeiconsIcon } from "@hugeicons/react"
import { ViewIcon, ViewOffSlashIcon, Shield01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useAuth } from "@/lib/auth"
import { ApiError } from "@/lib/api"

export const Route = createFileRoute("/login")({
  component: LoginPage,
})

function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [emailErr, setEmailErr] = useState("")
  const [passwordErr, setPasswordErr] = useState("")
  const [formErr, setFormErr] = useState("")

  const emailRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)

  const validateEmail = (val: string) => {
    if (!val.trim() || !val.includes("@")) {
      return "Enter a valid administrator email address."
    }
    return ""
  }

  const validatePassword = (val: string) => {
    if (val.length < 8) {
      return "Password must be at least 8 characters."
    }
    return ""
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    const eErr = validateEmail(email)
    const pErr = validatePassword(password)
    setEmailErr(eErr)
    setPasswordErr(pErr)
    setFormErr("")

    if (eErr) {
      emailRef.current?.focus()
      return
    }
    if (pErr) {
      passwordRef.current?.focus()
      return
    }

    setSubmitting(true)
    try {
      await login(email.trim(), password)
      await navigate({ to: "/" })
    } catch (err) {
      setFormErr(
        err instanceof ApiError ? err.message : "Unable to authenticate. Please try again."
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="min-h-dvh flex items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="flex items-center gap-2.5 mb-1">
            <div className="flex size-8 items-center justify-center bg-primary text-primary-foreground font-heading text-sm font-bold">
              R
            </div>
            <div>
              <CardTitle className="text-base">Runway</CardTitle>
              <CardDescription>Self-Hosted GitHub Runner Control Plane</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} method="POST" noValidate className="space-y-4">
            {formErr && (
              <div
                role="alert"
                aria-live="polite"
                className="border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
              >
                {formErr}
              </div>
            )}

            <div className="space-y-1.5">
              <Label htmlFor="login-email">Email</Label>
              <Input
                ref={emailRef}
                id="login-email"
                name="email"
                type="email"
                autoComplete="username"
                autoCorrect="off"
                autoCapitalize="none"
                spellCheck={false}
                required
                value={email}
                aria-invalid={Boolean(emailErr)}
                aria-describedby={emailErr ? "login-email-err" : undefined}
                onChange={(e) => {
                  setEmail(e.target.value)
                  if (emailErr) setEmailErr("")
                }}
                onBlur={() => setEmailErr(validateEmail(email))}
                placeholder="admin@runway.local"
              />
              {emailErr && (
                <p id="login-email-err" aria-live="polite" className="text-[11px] text-destructive">
                  {emailErr}
                </p>
              )}
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="login-password">Password</Label>
              <div className="relative flex items-center">
                <Input
                  ref={passwordRef}
                  id="login-password"
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="current-password"
                  minLength={8}
                  required
                  value={password}
                  aria-invalid={Boolean(passwordErr)}
                  aria-describedby={passwordErr ? "login-password-err" : undefined}
                  onChange={(e) => {
                    setPassword(e.target.value)
                    if (passwordErr) setPasswordErr("")
                  }}
                  onBlur={() => setPasswordErr(validatePassword(password))}
                  className="pr-9"
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  aria-pressed={showPassword}
                  aria-label={showPassword ? "Hide password" : "Show password"}
                  onClick={() => setShowPassword((v) => !v)}
                  className="absolute right-1.5"
                >
                  <HugeiconsIcon
                    icon={showPassword ? ViewOffSlashIcon : ViewIcon}
                    strokeWidth={2}
                    className="size-3.5"
                  />
                </Button>
              </div>
              {passwordErr && (
                <p
                  id="login-password-err"
                  aria-live="polite"
                  className="text-[11px] text-destructive"
                >
                  {passwordErr}
                </p>
              )}
            </div>

            <Button type="submit" disabled={submitting} className="w-full">
              <HugeiconsIcon icon={Shield01Icon} strokeWidth={2} />
              <span>{submitting ? "Signing in..." : "Sign in"}</span>
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  )
}
