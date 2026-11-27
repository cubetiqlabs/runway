import { useRef, useState } from "react"
import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useForm } from "@tanstack/react-form"
import { z } from "zod"
import { HugeiconsIcon } from "@hugeicons/react"
import { ViewIcon, ViewOffSlashIcon, Shield01Icon } from "@hugeicons/core-free-icons"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/lib/auth"
import { ApiError } from "@/lib/api"

export const Route = createFileRoute("/login")({
  component: LoginPage,
})

const loginSchema = z.object({
  email: z.string().trim().email("Enter a valid administrator email address."),
  password: z.string().min(8, "Password must be at least 8 characters."),
})

function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [showPassword, setShowPassword] = useState(false)
  const [formErr, setFormErr] = useState("")

  const emailRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)

  const form = useForm({
    defaultValues: {
      email: "",
      password: "",
    },
    validators: {
      onBlur: loginSchema,
      onSubmit: loginSchema,
    },
    onSubmitInvalid: ({ formApi }) => {
      const emailMeta = formApi.getFieldMeta("email")
      if (emailMeta && !emailMeta.isValid) {
        emailRef.current?.focus()
        return
      }
      passwordRef.current?.focus()
    },
    onSubmit: async ({ value }) => {
      setFormErr("")
      try {
        await login(value.email.trim(), value.password)
        await navigate({ to: "/" })
      } catch (err) {
        setFormErr(
          err instanceof ApiError ? err.message : "Unable to authenticate. Please try again."
        )
      }
    },
  })

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
          <form
            onSubmit={(e) => {
              e.preventDefault()
              e.stopPropagation()
              void form.handleSubmit()
            }}
            method="POST"
            noValidate
            className="space-y-4"
          >
            {formErr && (
              <div
                role="alert"
                aria-live="polite"
                className="border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
              >
                {formErr}
              </div>
            )}

            <FieldGroup className="gap-4">
              <form.Field name="email">
                {(field) => {
                  const isInvalid =
                    (field.state.meta.isTouched || form.state.submissionAttempts > 0) &&
                    !field.state.meta.isValid
                  return (
                    <Field data-invalid={isInvalid}>
                      <FieldLabel htmlFor="login-email">Email</FieldLabel>
                      <Input
                        ref={emailRef}
                        id="login-email"
                        name={field.name}
                        type="email"
                        autoComplete="username"
                        autoCorrect="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        required
                        value={field.state.value}
                        aria-invalid={isInvalid ? "true" : "false"}
                        aria-describedby={isInvalid ? "login-email-err" : undefined}
                        onBlur={field.handleBlur}
                        onChange={(e) => {
                          field.handleChange(e.target.value)
                          if (formErr) setFormErr("")
                        }}
                        placeholder="admin@runway.local"
                      />
                      {isInvalid && (
                        <FieldError id="login-email-err" errors={field.state.meta.errors} />
                      )}
                    </Field>
                  )
                }}
              </form.Field>

              <form.Field name="password">
                {(field) => {
                  const isInvalid =
                    (field.state.meta.isTouched || form.state.submissionAttempts > 0) &&
                    !field.state.meta.isValid
                  return (
                    <Field data-invalid={isInvalid}>
                      <FieldLabel htmlFor="login-password">Password</FieldLabel>
                      <div className="relative flex items-center">
                        <Input
                          ref={passwordRef}
                          id="login-password"
                          name={field.name}
                          type={showPassword ? "text" : "password"}
                          autoComplete="current-password"
                          minLength={8}
                          required
                          value={field.state.value}
                          aria-invalid={isInvalid ? "true" : "false"}
                          aria-describedby={isInvalid ? "login-password-err" : undefined}
                          onBlur={field.handleBlur}
                          onChange={(e) => {
                            field.handleChange(e.target.value)
                            if (formErr) setFormErr("")
                          }}
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
                      {isInvalid && (
                        <FieldError id="login-password-err" errors={field.state.meta.errors} />
                      )}
                    </Field>
                  )
                }}
              </form.Field>
            </FieldGroup>

            <form.Subscribe selector={(state) => state.isSubmitting}>
              {(isSubmitting) => (
                <Button type="submit" disabled={isSubmitting} className="w-full">
                  <HugeiconsIcon icon={Shield01Icon} strokeWidth={2} />
                  <span>{isSubmitting ? "Signing in..." : "Sign in"}</span>
                </Button>
              )}
            </form.Subscribe>
          </form>
        </CardContent>
      </Card>
    </main>
  )
}
