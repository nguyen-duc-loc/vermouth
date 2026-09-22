import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useBlocker } from '@tanstack/react-router'
import { CalendarDays, CheckCircle2, Home, Landmark, ReceiptText, Save, Users } from 'lucide-react'
import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { billingKeys } from '../api/billing'
import {
  bankKeys,
  type InvoiceProfile,
  ProfileApiError,
  profileKeys,
  putInvoiceProfile,
  readBanks,
  readInvoiceProfile,
} from '../api/profile'
import { readTutor } from '../api/teaching'
import { AccountPanel } from '../components/AccountPanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { ErrorState } from '../components/ErrorState'
import { FormField } from '../components/FormField'
import { PageEntrance } from '../components/PageEntrance'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader } from '../components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../components/ui/dialog'
import { Input } from '../components/ui/input'
import { Skeleton } from '../components/ui/skeleton'
import { toast } from '../components/ui/sonner'

const destinations: readonly AppDestination[] = [
  { href: '/', label: 'Home', icon: Home },
  { href: '/schedule', label: 'Schedule', icon: CalendarDays },
  { href: '/students', label: 'Students', icon: Users },
  { href: '/billing', label: 'Billing', icon: ReceiptText },
]

const fieldLabels = {
  legal_name: 'Legal invoice name',
  contact_line: 'Invoice contact',
  bank_code: 'Bank',
  bank_account_number: 'Account number',
  bank_account_holder: 'Account holder',
} as const

type EditableField = keyof typeof fieldLabels

type ProfileDraft = {
  legalName: string
  contactLine: string
  bankCode: string
  bankAccountNumber: string
  bankAccountHolder: string
}

type FieldErrors = Partial<Record<EditableField, string>>

const emptyDraft: ProfileDraft = {
  legalName: '',
  contactLine: '',
  bankCode: '',
  bankAccountNumber: '',
  bankAccountHolder: '',
}

/** Gives the tutor one private, revision guarded surface for future invoice identity. */
export function ProfilePage() {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<ProfileDraft>(emptyDraft)
  const [baseline, setBaseline] = useState<InvoiceProfile>()
  const [search, setSearch] = useState('')
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
  const [announcement, setAnnouncement] = useState('')
  const [confirmIncomplete, setConfirmIncomplete] = useState(false)
  const [conflictProfile, setConflictProfile] = useState<InvoiceProfile>()
  const pendingSave = useRef(false)
  const incompleteSaveTrigger = useRef<HTMLElement | null>(null)
  const dirtyNavigationTrigger = useRef<HTMLElement | null>(null)

  useEffect(() => {
    document.title = 'Profile and bank details · Vermouth'
  }, [])

  const tutorQuery = useQuery({ queryKey: ['tutor'], queryFn: ({ signal }) => readTutor(signal) })
  const tutorId = tutorQuery.data?.tutor_id ?? ''
  const profileQuery = useQuery({
    queryKey: profileKeys.detail(tutorId),
    queryFn: ({ signal }) => readInvoiceProfile(signal),
    enabled: tutorId !== '',
    staleTime: 0,
  })
  const bankQuery = useQuery({
    queryKey: bankKeys.list(tutorId),
    queryFn: ({ signal }) => readBanks(signal),
    enabled: tutorId !== '',
    staleTime: 86_400_000,
  })

  useEffect(() => {
    if (profileQuery.data && baseline === undefined) {
      setBaseline(profileQuery.data)
      setDraft(draftFromProfile(profileQuery.data))
    }
  }, [baseline, profileQuery.data])

  const normalizedDraft = useMemo(() => normalizeDraft(draft), [draft])
  const dirty = baseline !== undefined && !draftMatchesProfile(normalizedDraft, baseline)
  const missingFields = missingDraftFields(normalizedDraft)
  const clearedFields = baseline?.is_complete
    ? missingFields.filter((field) => profileValue(baseline, field) !== null)
    : []
  const canSaveWithoutCatalog =
    bankQuery.isError &&
    baseline?.bank_status === 'active' &&
    normalizedDraft.bankCode === (baseline.bank_code ?? '')
  const saveDisabled =
    baseline === undefined ||
    !dirty ||
    profileQuery.isError ||
    (bankQuery.isError && !canSaveWithoutCatalog)

  const filteredBanks = useMemo(() => {
    const foldedSearch = foldBankSearch(search)
    const banks = bankQuery.data?.banks ?? []
    if (foldedSearch === '') return banks
    return banks.filter((bank) =>
      [bank.code, bank.short_name, bank.official_name].some((value) =>
        foldBankSearch(value).includes(foldedSearch),
      ),
    )
  }, [bankQuery.data, search])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!baseline) throw new Error('the profile has not loaded')
      return putInvoiceProfile({
        expected_revision: baseline.revision,
        legal_name: nullable(normalizedDraft.legalName),
        contact_line: nullable(normalizedDraft.contactLine),
        bank_code: nullable(normalizedDraft.bankCode),
        bank_account_number: nullable(normalizedDraft.bankAccountNumber),
        bank_account_holder: nullable(normalizedDraft.bankAccountHolder),
      })
    },
    onSuccess: (profile) => {
      queryClient.setQueryData(profileKeys.detail(tutorId), profile)
      setBaseline(profile)
      setDraft(draftFromProfile(profile))
      setFieldErrors({})
      setConflictProfile(undefined)
      setAnnouncement('Profile saved.')
      toast.success('Profile saved')
      void queryClient.invalidateQueries({ queryKey: billingKeys.all })
    },
    onError: async (error) => {
      if (error instanceof ProfileApiError && error.body.error.code === 'invalid_profile') {
        const errors = profileFieldErrors(error.body.error.details)
        setFieldErrors(errors)
        focusFirstInvalid(errors)
        if (errors.bank_code?.includes('active')) void bankQuery.refetch()
        return
      }
      if (error instanceof ProfileApiError && error.body.error.code === 'profile_conflict') {
        try {
          setConflictProfile(await readInvoiceProfile())
        } catch {
          setConflictProfile(undefined)
        }
      }
    },
  })

  const blocker = useBlocker({
    shouldBlockFn: () => {
      const shouldBlock = dirty && !pendingSave.current
      if (shouldBlock && document.activeElement instanceof HTMLElement) {
        dirtyNavigationTrigger.current = document.activeElement
      }
      return shouldBlock
    },
    enableBeforeUnload: dirty,
    withResolver: true,
  })

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const localErrors = validateDraft(draft)
    setFieldErrors(localErrors)
    if (Object.keys(localErrors).length > 0) {
      focusFirstInvalid(localErrors)
      return
    }
    if (clearedFields.length > 0) {
      if (document.activeElement instanceof HTMLElement) {
        incompleteSaveTrigger.current = document.activeElement
      }
      setConfirmIncomplete(true)
      return
    }
    void runSave()
  }

  async function runSave() {
    pendingSave.current = true
    setConfirmIncomplete(false)
    try {
      await saveMutation.mutateAsync()
    } catch {
      return
    } finally {
      pendingSave.current = false
    }
  }

  function discardAndLoad(profile: InvoiceProfile) {
    setBaseline(profile)
    setDraft(draftFromProfile(profile))
    setFieldErrors({})
    setConflictProfile(undefined)
    queryClient.setQueryData(profileKeys.detail(tutorId), profile)
  }

  if (tutorQuery.isPending || profileQuery.isPending) {
    return <ProfileLoading />
  }

  if (tutorQuery.isError || profileQuery.isError || baseline === undefined) {
    return (
      <ProfileShell>
        <PageEntrance className="mx-auto grid w-full max-w-5xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
          <ProfileHeader />
          <ErrorState
            headingLevel="h2"
            title="Your profile could not be loaded"
            description="No empty form is shown because it could overwrite details already saved. Check the connection, then try again."
            action={
              <Button
                variant="secondary"
                onClick={() => {
                  void tutorQuery.refetch()
                  void profileQuery.refetch()
                }}
              >
                Retry
              </Button>
            }
          />
        </PageEntrance>
      </ProfileShell>
    )
  }

  return (
    <ProfileShell
      accountPanel={
        <AccountPanel
          beforeSignOut={() =>
            !dirty || window.confirm('Discard unsaved profile changes and sign out?')
          }
        />
      }
    >
      <PageEntrance className="mx-auto grid w-full max-w-5xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <ProfileHeader profile={baseline} />

        {conflictProfile ? (
          <Card data-entrance-item className="border-warning/40 bg-warning-surface">
            <CardHeader>
              <h2 className="text-lg font-semibold">Your profile changed in another tab</h2>
              <CardDescription>
                Your edits are still here. You can keep reviewing them, or discard them and load the
                latest saved profile.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 sm:flex-row">
              <Button variant="secondary" onClick={() => setConflictProfile(undefined)}>
                Keep editing
              </Button>
              <Button
                variant="quiet"
                onClick={() => {
                  if (window.confirm('Discard your edits and load the latest saved profile?')) {
                    discardAndLoad(conflictProfile)
                  }
                }}
              >
                Discard my edits and load latest
              </Button>
            </CardContent>
          </Card>
        ) : null}

        {saveMutation.isError && !conflictProfile && Object.keys(fieldErrors).length === 0 ? (
          <ErrorState
            headingLevel="h2"
            title="Your profile was not saved"
            description="Every value you entered is still here. Check the connection, then save again."
          />
        ) : null}

        <form className="grid gap-6" onSubmit={submit} noValidate>
          <Card data-entrance-item>
            <CardHeader>
              <h2 className="text-lg font-semibold">Invoice identity</h2>
              <CardDescription>
                Use the name and contact line you want parents to see on every future invoice.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-5 md:grid-cols-2">
              <FormField
                controlId="legal_name"
                label={fieldLabels.legal_name}
                hint="Up to 120 characters. Letter case is kept as you enter it."
                error={fieldErrors.legal_name}
                control={(accessibility) => (
                  <Input
                    {...accessibility}
                    value={draft.legalName}
                    disabled={saveMutation.isPending}
                    autoComplete="name"
                    onChange={(event) => setDraftField('legalName', event.currentTarget.value)}
                  />
                )}
              />
              <FormField
                controlId="contact_line"
                label={fieldLabels.contact_line}
                hint="For example, a phone number or email address parents can use."
                error={fieldErrors.contact_line}
                control={(accessibility) => (
                  <Input
                    {...accessibility}
                    value={draft.contactLine}
                    disabled={saveMutation.isPending}
                    onChange={(event) => setDraftField('contactLine', event.currentTarget.value)}
                  />
                )}
              />
            </CardContent>
          </Card>

          <Card data-entrance-item>
            <CardHeader>
              <h2 className="text-lg font-semibold">Bank details</h2>
              <CardDescription>
                Choose the receiving bank, then enter the account exactly as it should appear with
                payment instructions.
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-6">
              {baseline.bank_status === 'inactive' ? (
                <div
                  role="alert"
                  className="rounded-lg border border-warning/40 bg-warning-surface p-4 text-sm text-warning-foreground"
                >
                  The saved bank is no longer available for new payments. Its details remain
                  visible, but choose an active bank before saving any change.
                </div>
              ) : null}

              <FormField
                controlId="bank-search"
                label="Search banks"
                hint="Search by payment code, familiar short name, or official Vietnamese name."
                error={fieldErrors.bank_code}
                control={(accessibility) => (
                  <Input
                    {...accessibility}
                    type="search"
                    value={search}
                    disabled={saveMutation.isPending || bankQuery.isError}
                    onChange={(event) => setSearch(event.currentTarget.value)}
                  />
                )}
              />

              {bankQuery.isPending ? (
                <div role="status" aria-live="polite" className="grid gap-2">
                  <Skeleton className="h-14 w-full" />
                  <Skeleton className="h-14 w-full" />
                  <span className="sr-only">Loading banks</span>
                </div>
              ) : bankQuery.isError ? (
                <ErrorState
                  title="The bank list could not be loaded"
                  description="Your saved profile is still here. Bank changes are disabled until the list is available."
                  action={
                    <Button variant="secondary" onClick={() => void bankQuery.refetch()}>
                      Retry bank list
                    </Button>
                  }
                />
              ) : (
                <fieldset disabled={saveMutation.isPending} className="grid gap-3">
                  <legend className="text-sm font-medium">Receiving bank</legend>
                  {baseline.bank_code === null && baseline.bank_name ? (
                    <p className="rounded-lg border border-border bg-muted p-3 text-sm text-muted-foreground">
                      Previous bank label: {baseline.bank_name}. Choose a current bank to replace
                      it.
                    </p>
                  ) : null}
                  <div className="max-h-72 overflow-y-auto rounded-xl border border-border bg-surface p-2">
                    {filteredBanks.length > 0 ? (
                      <ul className="grid gap-1">
                        {filteredBanks.map((bank) => {
                          const id = `bank-${bank.code}`
                          return (
                            <li key={bank.code}>
                              <label
                                htmlFor={id}
                                className="flex min-h-14 cursor-pointer items-center gap-3 rounded-lg px-3 py-2 transition-colors duration-base hover:bg-muted has-[:checked]:bg-primary/10 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-focus motion-reduce:transition-none"
                              >
                                <input
                                  id={id}
                                  type="radio"
                                  name="bank_code"
                                  value={bank.code}
                                  checked={draft.bankCode === bank.code}
                                  className="size-5 accent-primary"
                                  onChange={() => setDraftField('bankCode', bank.code)}
                                />
                                <span className="min-w-0 flex-1">
                                  <span className="block font-medium">{bank.short_name}</span>
                                  <span className="block text-sm leading-relaxed text-muted-foreground wrap-anywhere">
                                    {bank.official_name}
                                  </span>
                                </span>
                                <span className="font-mono text-xs text-muted-foreground">
                                  {bank.code}
                                </span>
                              </label>
                            </li>
                          )
                        })}
                      </ul>
                    ) : (
                      <p className="p-4 text-sm text-muted-foreground">
                        No bank matches this search.
                      </p>
                    )}
                  </div>
                </fieldset>
              )}

              <div className="grid gap-5 md:grid-cols-2">
                <FormField
                  controlId="bank_account_number"
                  label={fieldLabels.bank_account_number}
                  hint="3 to 34 letters or numbers. Spaces and punctuation are not accepted."
                  error={fieldErrors.bank_account_number}
                  control={(accessibility) => (
                    <Input
                      {...accessibility}
                      value={draft.bankAccountNumber}
                      disabled={saveMutation.isPending}
                      autoCapitalize="characters"
                      inputMode="text"
                      onChange={(event) =>
                        setDraftField('bankAccountNumber', event.currentTarget.value)
                      }
                    />
                  )}
                />
                <FormField
                  controlId="bank_account_holder"
                  label={fieldLabels.bank_account_holder}
                  hint="Use the holder name registered with the bank."
                  error={fieldErrors.bank_account_holder}
                  control={(accessibility) => (
                    <Input
                      {...accessibility}
                      value={draft.bankAccountHolder}
                      disabled={saveMutation.isPending}
                      autoComplete="name"
                      onChange={(event) =>
                        setDraftField('bankAccountHolder', event.currentTarget.value)
                      }
                    />
                  )}
                />
              </div>
            </CardContent>
          </Card>

          <Card data-entrance-item className="shadow-raised">
            <CardContent className="flex flex-col gap-4 pt-5 sm:flex-row sm:items-center sm:justify-between md:pt-6">
              <div className="grid gap-1">
                <p className="font-medium">Save one private invoice profile</p>
                <p className="text-sm leading-relaxed text-muted-foreground">
                  Partial progress is allowed. Future invoice creation waits until every field is
                  complete.
                </p>
              </div>
              <Button
                type="submit"
                size="large"
                className="w-full sm:w-auto"
                disabled={saveDisabled}
                loading={saveMutation.isPending}
              >
                <Save aria-hidden="true" className="size-icon-md" />
                {saveMutation.isPending ? 'Saving profile…' : 'Save profile'}
              </Button>
            </CardContent>
          </Card>
        </form>

        <p role="status" aria-live="polite" className="sr-only">
          {announcement}
        </p>
      </PageEntrance>

      <Dialog
        open={confirmIncomplete}
        onOpenChange={(open) => {
          if (open) setConfirmIncomplete(true)
          else closeIncompleteConfirmation()
        }}
      >
        <DialogContent closeLabel="Close incomplete profile confirmation">
          <DialogHeader>
            <DialogTitle>Save an incomplete profile?</DialogTitle>
            <DialogDescription>
              Future invoice creation will be blocked until you fill these fields again:{' '}
              {clearedFields.map((field) => fieldLabels[field]).join(', ')}.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="quiet" onClick={closeIncompleteConfirmation}>
              Keep editing
            </Button>
            <Button onClick={() => void runSave()}>Save incomplete profile</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={blocker.status === 'blocked'}
        onOpenChange={(open) => {
          if (!open) stayOnProfile()
        }}
      >
        <DialogContent closeLabel="Stay on profile page">
          <DialogHeader>
            <DialogTitle>Discard unsaved profile changes?</DialogTitle>
            <DialogDescription>
              Your latest edits have not been saved. You can stay here or discard them and continue.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="quiet" onClick={stayOnProfile}>
              Stay and keep editing
            </Button>
            <Button
              variant="destructive"
              onClick={() => blocker.status === 'blocked' && blocker.proceed()}
            >
              Discard and leave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ProfileShell>
  )

  function setDraftField<Field extends keyof ProfileDraft>(
    field: Field,
    value: ProfileDraft[Field],
  ) {
    setDraft((current) => ({ ...current, [field]: value }))
    const errorField = draftFieldCode(field)
    if (errorField) setFieldErrors((current) => ({ ...current, [errorField]: undefined }))
  }

  function stayOnProfile() {
    if (blocker.status !== 'blocked') return
    const trigger = dirtyNavigationTrigger.current
    blocker.reset()
    window.setTimeout(() => trigger?.focus())
  }

  function closeIncompleteConfirmation() {
    const trigger = incompleteSaveTrigger.current
    setConfirmIncomplete(false)
    window.setTimeout(() => trigger?.focus())
  }
}

function ProfileShell({
  children,
  accountPanel,
}: {
  children: ReactNode
  accountPanel?: ReactNode
}) {
  return (
    <AppShell
      brandName="Vermouth"
      text={{
        skipToContent: 'Skip to profile',
        primaryNavigation: 'Primary navigation',
        moreActions: 'More destinations',
        account: 'Account and appearance',
        accountDescription: 'Manage invoice details, appearance, or the current session.',
        closeAccount: 'Close account panel',
      }}
      primaryDestinations={destinations}
      appearancePanel={accountPanel ?? <AccountPanel />}
    >
      {children}
    </AppShell>
  )
}

function ProfileHeader({ profile }: { profile?: InvoiceProfile }) {
  return (
    <header
      data-entrance-item
      className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end"
    >
      <div className="grid max-w-3xl gap-3">
        <Badge variant="primary" className="w-fit">
          <Landmark aria-hidden="true" className="size-icon-sm" />
          Invoice profile
        </Badge>
        <h1 className="text-3xl font-semibold text-balance">Set the details parents will trust.</h1>
        <p className="text-base leading-relaxed text-muted-foreground">
          Keep your invoice identity and receiving account together. Bank values stay private and
          never appear in the account menu.
        </p>
      </div>
      {profile ? (
        <div className="flex min-h-11 items-center gap-2 rounded-xl border border-border bg-surface px-4 py-3">
          {profile.is_complete && profile.bank_status === 'active' ? (
            <>
              <CheckCircle2 aria-hidden="true" className="size-icon-md text-success" />
              <span className="font-medium">Complete</span>
            </>
          ) : (
            <>
              <Badge variant="warning">Incomplete</Badge>
              <span className="text-sm text-muted-foreground">
                {profile.missing_fields.length} fields remaining
              </span>
            </>
          )}
        </div>
      ) : null}
    </header>
  )
}

function ProfileLoading() {
  return (
    <ProfileShell>
      <PageEntrance className="mx-auto grid w-full max-w-5xl gap-7 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <ProfileHeader />
        <div role="status" aria-live="polite" aria-busy="true" className="grid gap-6">
          <Skeleton className="h-72 w-full rounded-xl" />
          <Skeleton className="h-96 w-full rounded-xl" />
          <span className="sr-only">Loading profile and bank details</span>
        </div>
      </PageEntrance>
    </ProfileShell>
  )
}

function draftFromProfile(profile: InvoiceProfile): ProfileDraft {
  return {
    legalName: profile.legal_name ?? '',
    contactLine: profile.contact_line ?? '',
    bankCode: profile.bank_code ?? '',
    bankAccountNumber: profile.bank_account_number ?? '',
    bankAccountHolder: profile.bank_account_holder ?? '',
  }
}

function normalizeDraft(draft: ProfileDraft): ProfileDraft {
  return {
    legalName: normalizeText(draft.legalName),
    contactLine: normalizeText(draft.contactLine),
    bankCode: draft.bankCode.trim(),
    bankAccountNumber: draft.bankAccountNumber.trim().toUpperCase(),
    bankAccountHolder: normalizeText(draft.bankAccountHolder),
  }
}

function normalizeText(value: string) {
  return value.normalize('NFC').trim().replace(/\s+/gu, ' ')
}

function draftMatchesProfile(draft: ProfileDraft, profile: InvoiceProfile) {
  return (
    nullable(draft.legalName) === profile.legal_name &&
    nullable(draft.contactLine) === profile.contact_line &&
    nullable(draft.bankCode) === profile.bank_code &&
    nullable(draft.bankAccountNumber) === profile.bank_account_number &&
    nullable(draft.bankAccountHolder) === profile.bank_account_holder
  )
}

function nullable(value: string): string | null {
  return value === '' ? null : value
}

function missingDraftFields(draft: ProfileDraft): EditableField[] {
  const missing: EditableField[] = []
  if (!draft.legalName) missing.push('legal_name')
  if (!draft.contactLine) missing.push('contact_line')
  if (!draft.bankCode) missing.push('bank_code')
  if (!draft.bankAccountNumber) missing.push('bank_account_number')
  if (!draft.bankAccountHolder) missing.push('bank_account_holder')
  return missing
}

function profileValue(profile: InvoiceProfile, field: EditableField): string | null {
  if (field === 'legal_name') return profile.legal_name
  if (field === 'contact_line') return profile.contact_line
  if (field === 'bank_code') return profile.bank_code
  if (field === 'bank_account_number') return profile.bank_account_number
  return profile.bank_account_holder
}

function validateDraft(draft: ProfileDraft): FieldErrors {
  const errors: FieldErrors = {}
  validateText(draft.legalName, 'legal_name', 120, errors)
  validateText(draft.contactLine, 'contact_line', 200, errors)
  validateText(draft.bankAccountHolder, 'bank_account_holder', 120, errors)
  const account = draft.bankAccountNumber.trim()
  if (account !== '' && !/^[A-Za-z0-9]{3,34}$/.test(account)) {
    errors.bank_account_number = 'Use 3 to 34 letters or numbers, with no spaces or punctuation.'
  }
  return errors
}

function validateText(value: string, field: EditableField, maximum: number, errors: FieldErrors) {
  if ([...value].some((character) => /\p{Cc}/u.test(character))) {
    errors[field] = 'Remove line breaks and control characters.'
    return
  }
  if ([...normalizeText(value)].length > maximum) {
    errors[field] = `Use ${maximum} characters or fewer.`
  }
}

function profileFieldErrors(details: unknown): FieldErrors {
  if (!details || typeof details !== 'object' || !('fields' in details)) return {}
  const fields = (details as { fields?: unknown }).fields
  if (!Array.isArray(fields)) return {}
  const errors: FieldErrors = {}
  for (const item of fields) {
    if (!item || typeof item !== 'object') continue
    const field = (item as { field?: unknown }).field
    const reason = (item as { reason?: unknown }).reason
    if (!isEditableField(field) || typeof reason !== 'string') continue
    errors[field] = validationMessage(reason)
  }
  return errors
}

function validationMessage(reason: string) {
  if (reason === 'too_long') return 'This value is too long.'
  if (reason === 'inactive_bank') return 'Choose an active bank before saving changes.'
  return 'Use the format described below this field.'
}

function isEditableField(value: unknown): value is EditableField {
  return typeof value === 'string' && value in fieldLabels
}

function focusFirstInvalid(errors: FieldErrors) {
  const first = (Object.keys(fieldLabels) as EditableField[]).find((field) => errors[field])
  if (!first) return
  window.setTimeout(() =>
    document.getElementById(first === 'bank_code' ? 'bank-search' : first)?.focus(),
  )
}

function foldBankSearch(value: string) {
  return value.normalize('NFD').replace(/\p{M}/gu, '').replace(/[đĐ]/g, 'd').toLowerCase().trim()
}

function draftFieldCode(field: keyof ProfileDraft): EditableField | undefined {
  if (field === 'legalName') return 'legal_name'
  if (field === 'contactLine') return 'contact_line'
  if (field === 'bankCode') return 'bank_code'
  if (field === 'bankAccountNumber') return 'bank_account_number'
  if (field === 'bankAccountHolder') return 'bank_account_holder'
  return undefined
}
