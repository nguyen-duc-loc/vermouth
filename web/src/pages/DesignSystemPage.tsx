import { enUS, vi } from 'date-fns/locale'
import {
  Bell,
  BookOpen,
  CalendarDays,
  Check,
  CircleHelp,
  Clock3,
  FileText,
  Home,
  MoreHorizontal,
  Palette,
  Plus,
  Settings,
  Sparkles,
  Users,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { AppearancePanel, type AppearancePanelText } from '../components/AppearancePanel'
import { type AppDestination, AppShell } from '../components/AppShell'
import { ClassColorCard } from '../components/ClassColorCard'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FormField } from '../components/FormField'
import { IconButton } from '../components/IconButton'
import { PageEntrance } from '../components/PageEntrance'
import { ResponsiveTable } from '../components/ResponsiveTable'
import { Alert, AlertDescription, AlertTitle } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '../components/ui/card'
import { Checkbox } from '../components/ui/checkbox'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '../components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../components/ui/dropdown-menu'
import { Input } from '../components/ui/input'
import { Label } from '../components/ui/label'
import { RadioGroup, RadioGroupItem } from '../components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '../components/ui/select'
import { Separator } from '../components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '../components/ui/sheet'
import { Skeleton } from '../components/ui/skeleton'
import { toast } from '../components/ui/sonner'
import { Switch } from '../components/ui/switch'
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs'
import { Tooltip, TooltipContent, TooltipTrigger } from '../components/ui/tooltip'
import { CLASS_COLOR_LABELS, CLASS_COLORS } from '../design-system/class-colors'
import { formatLocalDate } from '../lib/date'

const appearanceText: AppearancePanelText = {
  title: 'Giao diện',
  themeLegend: 'Chế độ sáng tối',
  accentLegend: 'Màu nhấn',
  themes: {
    light: 'Sáng',
    dark: 'Tối',
    system: 'Theo máy',
  },
  accents: {
    red: 'Đỏ',
    rose: 'Hồng',
    orange: 'Cam',
    green: 'Xanh lá',
    blue: 'Xanh dương',
    yellow: 'Vàng',
    violet: 'Tím',
  },
}

const shellText = {
  skipToContent: 'Bỏ qua điều hướng',
  primaryNavigation: 'Điều hướng chính',
  moreActions: 'Thêm mục',
  account: 'Tài khoản và giao diện',
  accountDescription: 'Điều chỉnh giao diện trên thiết bị này.',
  closeAccount: 'Đóng bảng tài khoản',
}

const destinations: readonly AppDestination[] = [
  { href: '/design-system', label: 'Nền tảng', icon: Home },
  { href: '/design-system#forms', label: 'Biểu mẫu', icon: FileText },
  { href: '/design-system#classes', label: 'Lớp học', icon: BookOpen },
  { href: '/design-system#data', label: 'Dữ liệu', icon: Users },
  { href: '/design-system#feedback', label: 'Phản hồi', icon: Bell },
  { href: '/design-system#overlays', label: 'Lớp nổi', icon: Sparkles },
]

const secondaryDestinations: readonly AppDestination[] = [
  { href: '/design-system#motion', label: 'Chuyển động', icon: Palette },
  { href: '/design-system#settings', label: 'Thiết lập', icon: Settings },
]

type GalleryRow = {
  id: string
  student: string
  className: string
  status: string
}

const galleryRows: readonly GalleryRow[] = [
  { id: 'student-1', student: 'Nguyễn Minh Anh', className: 'Toán 8A', status: 'Có mặt' },
  {
    id: 'student-2',
    student: 'Trần Hoàng Bảo Châu',
    className: 'Vật lý 10',
    status: 'Chờ điểm danh',
  },
]

function GalleryStudentTable({
  rows,
  emptyState,
}: {
  rows: readonly GalleryRow[]
  emptyState: React.ReactNode
}) {
  return (
    <ResponsiveTable
      rows={rows}
      getRowKey={(row) => row.id}
      emptyState={emptyState}
      renderCard={(row) => (
        <Card>
          <CardHeader>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <CardTitle>{row.student}</CardTitle>
              <Badge variant={row.status === 'Có mặt' ? 'success' : 'warning'}>{row.status}</Badge>
            </div>
            <CardDescription>{row.className}</CardDescription>
          </CardHeader>
        </Card>
      )}
      renderTable={(tableRows) => (
        <Card className="overflow-hidden">
          <Table>
            <TableCaption>Danh sách mẫu dùng cho kiểm tra giao diện.</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Học sinh</TableHead>
                <TableHead scope="col">Lớp</TableHead>
                <TableHead scope="col">Trạng thái</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tableRows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell className="font-medium">{row.student}</TableCell>
                  <TableCell>{row.className}</TableCell>
                  <TableCell>
                    <Badge variant={row.status === 'Có mặt' ? 'success' : 'warning'}>
                      {row.status}
                    </Badge>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    />
  )
}

function Section({
  id,
  eyebrow,
  title,
  description,
  children,
}: {
  id: string
  eyebrow: string
  title: string
  description: string
  children: React.ReactNode
}) {
  return (
    <section id={id} data-entrance-item aria-labelledby={`${id}-title`} className="scroll-mt-20">
      <header className="mb-5 grid max-w-3xl gap-2">
        <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">{eyebrow}</p>
        <h2 id={`${id}-title`} className="text-2xl font-semibold text-balance">
          {title}
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
      </header>
      {children}
    </section>
  )
}

function ScheduleSignature() {
  return (
    <div className="grid gap-3 rounded-xl border border-border bg-surface p-3 shadow-raised sm:grid-cols-2 lg:grid-cols-4">
      <div className="grid gap-2 rounded-lg bg-muted p-3 sm:col-span-2 lg:col-span-1">
        <span className="font-mono text-xs text-muted-foreground">07:30</span>
        <ClassColorCard color="blue" title="Toán 8A" detail="12 học sinh" />
      </div>
      <div className="grid gap-2 rounded-lg bg-muted p-3">
        <span className="font-mono text-xs text-muted-foreground">09:15</span>
        <ClassColorCard color="green" title="Vật lý 10" detail="8 học sinh" />
      </div>
      <div className="grid gap-2 rounded-lg bg-muted p-3">
        <span className="font-mono text-xs text-muted-foreground">14:00</span>
        <ClassColorCard color="rose" title="Ôn thi cuối kỳ" detail="6 học sinh" />
      </div>
      <div className="grid content-center gap-2 rounded-lg border border-dashed border-border-strong p-4 text-center">
        <Clock3 aria-hidden="true" className="mx-auto size-icon-lg text-muted-foreground" />
        <span className="text-sm font-medium">Khoảng trống để chuẩn bị bài</span>
      </div>
    </div>
  )
}

type ScheduleGridCell = {
  title: string
  detail: string
  color: (typeof CLASS_COLORS)[number]
}

const scheduleDays = ['Thứ hai', 'Thứ ba', 'Thứ tư', 'Thứ năm', 'Thứ sáu', 'Thứ bảy', 'Chủ nhật']

const scheduleGridRows: readonly {
  time: string
  cells: readonly (ScheduleGridCell | null)[]
}[] = [
  {
    time: '07:30',
    cells: [
      { title: 'Toán 8A', detail: '12 học sinh', color: 'blue' },
      null,
      { title: 'Vật lý 10', detail: '8 học sinh', color: 'green' },
      { title: 'Toán 8A', detail: '12 học sinh', color: 'blue' },
      null,
      { title: 'Ôn thi cuối kỳ', detail: '6 học sinh', color: 'rose' },
      null,
    ],
  },
  {
    time: '14:00',
    cells: [
      null,
      { title: 'Hóa học 11', detail: '10 học sinh', color: 'orange' },
      null,
      { title: 'Ôn thi cuối kỳ', detail: '6 học sinh', color: 'rose' },
      { title: 'Tiếng Anh 9', detail: '9 học sinh', color: 'violet' },
      null,
      { title: 'Sinh học 12', detail: '7 học sinh', color: 'yellow' },
    ],
  },
]

function ScheduleGridExample() {
  const buttonRefs = useRef<Array<HTMLButtonElement | null>>([])
  const [activeCell, setActiveCell] = useState(0)
  const columnCount = scheduleDays.length

  const moveFocus = (nextIndex: number) => {
    setActiveCell(nextIndex)
    buttonRefs.current[nextIndex]?.focus()
  }

  const handleCellKeyDown = (event: React.KeyboardEvent<HTMLButtonElement>, index: number) => {
    const rowStart = Math.floor(index / columnCount) * columnCount
    let nextIndex: number | undefined

    switch (event.key) {
      case 'ArrowLeft':
        nextIndex = index % columnCount === 0 ? index : index - 1
        break
      case 'ArrowRight':
        nextIndex = index % columnCount === columnCount - 1 ? index : index + 1
        break
      case 'ArrowUp':
        nextIndex = Math.max(0, index - columnCount)
        break
      case 'ArrowDown':
        nextIndex = Math.min(scheduleGridRows.length * columnCount - 1, index + columnCount)
        break
      case 'Home':
        nextIndex = rowStart
        break
      case 'End':
        nextIndex = rowStart + columnCount - 1
        break
    }

    if (nextIndex === undefined || nextIndex === index) return
    event.preventDefault()
    moveFocus(nextIndex)
  }

  return (
    <div className="mt-6 grid gap-3">
      <div className="grid gap-1">
        <h3 id="schedule-grid-title" className="text-base font-semibold">
          Lưới lịch học có hai chiều
        </h3>
        <p id="schedule-grid-help" className="text-sm leading-relaxed text-muted-foreground">
          Cuộn ngang trên màn hình nhỏ. Dùng các phím mũi tên, Home và End để đi qua từng ô.
        </p>
      </div>
      <div
        data-schedule-grid-scroll
        className="overflow-x-auto rounded-xl border border-border bg-surface shadow-field"
      >
        <table
          // biome-ignore lint/a11y/noNoninteractiveElementToInteractiveRole: the semantic table uses the WAI ARIA grid keyboard model.
          role="grid"
          aria-labelledby="schedule-grid-title"
          aria-describedby="schedule-grid-help"
          aria-rowcount={scheduleGridRows.length + 1}
          aria-colcount={columnCount + 1}
          className="w-full min-w-4xl border-separate border-spacing-0"
        >
          <thead>
            <tr>
              <th
                scope="col"
                className="sticky start-0 z-10 border-e border-b border-border bg-muted p-3 text-start text-xs font-semibold text-muted-foreground"
              >
                Giờ
              </th>
              {scheduleDays.map((day, index) => (
                <th
                  key={day}
                  id={`schedule-day-${index}`}
                  scope="col"
                  className="border-b border-border bg-muted p-3 text-start text-xs font-semibold text-muted-foreground"
                >
                  {day}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {scheduleGridRows.map((row, rowIndex) => (
              <tr key={row.time}>
                <th
                  id={`schedule-time-${rowIndex}`}
                  scope="row"
                  className="sticky start-0 z-10 border-e border-b border-border bg-surface p-3 text-start font-mono text-xs font-semibold"
                >
                  {row.time}
                </th>
                {row.cells.map((cell, dayIndex) => {
                  const index = rowIndex * columnCount + dayIndex
                  const label = `${scheduleDays[dayIndex]}, ${row.time}: ${cell?.title ?? 'Trống'}`
                  return (
                    <td
                      key={`${row.time}-${scheduleDays[dayIndex]}`}
                      className="w-28 border-b border-border p-2"
                    >
                      <button
                        ref={(element) => {
                          buttonRefs.current[index] = element
                        }}
                        type="button"
                        aria-label={label}
                        data-class-color={cell?.color}
                        tabIndex={activeCell === index ? 0 : -1}
                        onFocus={() => setActiveCell(index)}
                        onKeyDown={(event) => handleCellKeyDown(event, index)}
                        className={
                          cell
                            ? 'relative grid min-h-16 w-full overflow-hidden rounded-md border border-class-border bg-class-surface px-3 py-2 text-start text-foreground outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background'
                            : 'grid min-h-16 w-full place-items-center rounded-md border border-dashed border-border-strong bg-surface px-3 py-2 text-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-focus focus-visible:ring-offset-2 focus-visible:ring-offset-background'
                        }
                      >
                        {cell ? (
                          <>
                            <span
                              aria-hidden="true"
                              className="absolute inset-y-0 start-0 w-1.5 bg-class-marker"
                            />
                            <span className="text-sm font-semibold">{cell.title}</span>
                            <span className="text-xs text-muted-foreground">{cell.detail}</span>
                          </>
                        ) : (
                          'Trống'
                        )}
                      </button>
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

/** Development only gallery for every foundation component and state. */
export function DesignSystemPage() {
  const [checked, setChecked] = useState(true)
  const [notices, setNotices] = useState(false)

  useEffect(() => {
    document.title = 'Design system · Vermouth'
  }, [])

  return (
    <AppShell
      brandName="Vermouth"
      text={shellText}
      primaryDestinations={destinations}
      secondaryDestinations={secondaryDestinations}
      appearancePanel={<AppearancePanel text={appearanceText} />}
      contextualPanel={
        <div className="grid gap-5">
          <div className="grid gap-1">
            <p className="text-xs font-semibold uppercase tracking-[0.14em] text-primary">
              Hôm nay
            </p>
            <p className="font-mono text-sm">
              {formatLocalDate(new Date(2026, 7, 27), 'dd.MM.yyyy', vi)}
            </p>
          </div>
          <Separator />
          <div className="grid gap-2">
            <p className="text-sm font-semibold">Nguyên tắc đang xem</p>
            <p className="text-sm leading-relaxed text-muted-foreground">
              Màu lớp học luôn đi cùng tên lớp và một vạch cạnh rõ ràng.
            </p>
          </div>
        </div>
      }
    >
      <PageEntrance className="mx-auto grid w-full max-w-7xl gap-14 px-4 py-8 sm:px-6 md:py-10 lg:px-8">
        <header
          data-entrance-item
          className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(18rem,28rem)] lg:items-end"
        >
          <div className="grid max-w-3xl gap-3">
            <Badge variant="primary" className="w-fit">
              Chỉ có trong môi trường phát triển
            </Badge>
            <h1 className="text-3xl font-semibold text-balance">
              Một ngôn ngữ chung cho ngày dạy học
            </h1>
            <p className="text-base leading-relaxed text-muted-foreground">
              Đây là nơi kiểm tra kiểu chữ, màu, thành phần, trạng thái và cách bố cục phản ứng
              trước màn hình nhỏ. Hãy thử bàn phím, phóng to 200 phần trăm, đổi chế độ và chọn mọi
              màu nhấn.
            </p>
          </div>
          <Card className="shadow-raised">
            <CardHeader>
              <CardTitle>Ngày mẫu</CardTitle>
              <CardDescription>
                {formatLocalDate(new Date(2026, 7, 27), 'EEEE, d MMMM yyyy', vi)}
              </CardDescription>
            </CardHeader>
            <CardContent className="font-mono text-sm text-muted-foreground">
              {formatLocalDate(new Date(2026, 7, 27), 'EEEE, MMMM d, yyyy', enUS)}
            </CardContent>
          </Card>
        </header>

        <ScheduleSignature />

        <Section
          id="foundations"
          eyebrow="01 · Nền tảng"
          title="Màu nhấn là lựa chọn, trạng thái là ý nghĩa"
          description="Chế độ sáng tối và bảy màu nhấn thay đổi cảm giác của sản phẩm mà không đổi ý nghĩa thành công, cảnh báo hay lỗi."
        >
          <Card>
            <CardContent className="grid gap-6 pt-5 md:grid-cols-[minmax(0,1fr)_minmax(18rem,28rem)] md:pt-6">
              <AppearancePanel text={appearanceText} />
              <div className="grid content-start gap-3">
                <Alert variant="success">
                  <AlertTitle>Đã ghi nhận điểm danh</AlertTitle>
                  <AlertDescription>
                    Thay màu nhấn vẫn giữ nguyên ý nghĩa thành công này.
                  </AlertDescription>
                </Alert>
                <Alert variant="warning">
                  <AlertTitle>Chưa đủ thông tin</AlertTitle>
                  <AlertDescription>
                    Hãy kiểm tra mức học phí trước khi tạo hóa đơn.
                  </AlertDescription>
                </Alert>
                <Alert variant="destructive" role="alert">
                  <AlertTitle>Không lưu được thay đổi</AlertTitle>
                  <AlertDescription>
                    Kết nối đã ngắt. Hãy thử lưu lại khi mạng ổn định.
                  </AlertDescription>
                </Alert>
              </div>
            </CardContent>
          </Card>
        </Section>

        <Section
          id="actions"
          eyebrow="02 · Hành động"
          title="Một hành động chính, các lựa chọn còn lại lùi đúng mức"
          description="Mỗi nút có trạng thái mặc định, trỏ chuột, nhấn, bàn phím, tắt và đang xử lý. Nút biểu tượng luôn có tên dễ tiếp cận."
        >
          <Card>
            <CardContent className="flex flex-wrap items-center gap-3 pt-5 md:pt-6">
              <Button>Lưu thay đổi</Button>
              <Button variant="secondary">Xem trước</Button>
              <Button variant="quiet">Hủy</Button>
              <Button variant="destructive">Xóa buổi học</Button>
              <Button disabled>Không thể chọn</Button>
              <Button loading>Đang lưu</Button>
              <IconButton
                label="Thêm buổi học"
                icon={<Plus aria-hidden="true" className="size-icon-md" />}
              />
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="secondary">
                    Thêm lựa chọn
                    <MoreHorizontal aria-hidden="true" className="size-icon-sm" />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start">
                  <DropdownMenuLabel>Thao tác với lớp</DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem>Đổi tên lớp</DropdownMenuItem>
                  <DropdownMenuItem>Tạm dừng lịch học</DropdownMenuItem>
                  <DropdownMenuItem disabled>Chuyển cho gia sư khác</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </CardContent>
          </Card>
        </Section>

        <Section
          id="forms"
          eyebrow="03 · Biểu mẫu"
          title="Nhãn luôn còn đó, lỗi luôn chỉ ra đường sửa"
          description="Ví dụ cố tình dùng một nhãn tiếng Việt dài để kiểm tra xuống dòng trên điện thoại và khi phóng to."
        >
          <div className="grid gap-5 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Thông tin lớp học</CardTitle>
                <CardDescription>
                  Mọi dòng chữ nhìn thấy đều do màn hình gọi thành phần cung cấp.
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-5">
                <FormField
                  controlId="gallery-class-name"
                  label="Tên lớp học dùng để phân biệt trong lịch và trên hóa đơn"
                  hint="Tên có thể dùng tiếng Việt đầy đủ dấu và sẽ xuống dòng khi cần."
                  requiredText="(bắt buộc)"
                  control={(accessibility) => (
                    <Input {...accessibility} defaultValue="Luyện thi Toán lớp 9" />
                  )}
                />
                <FormField
                  controlId="gallery-rate"
                  label="Học phí mỗi buổi"
                  error="Học phí phải lớn hơn 0 đồng."
                  control={(accessibility) => (
                    <Input
                      {...accessibility}
                      inputMode="numeric"
                      defaultValue="0"
                      className="font-mono"
                    />
                  )}
                />
                <FormField
                  controlId="gallery-status"
                  label="Trạng thái lớp"
                  control={(accessibility) => (
                    <Select defaultValue="active">
                      <SelectTrigger {...accessibility}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="active">Đang học</SelectItem>
                        <SelectItem value="paused">Tạm dừng</SelectItem>
                        <SelectItem value="finished">Đã kết thúc</SelectItem>
                      </SelectContent>
                    </Select>
                  )}
                />
                <Input
                  disabled
                  value="Trường đã bị khóa"
                  aria-label="Ví dụ trường bị khóa"
                  readOnly
                />
              </CardContent>
              <CardFooter>
                <Button>Lưu lớp học</Button>
                <Button variant="secondary">Hủy thay đổi</Button>
              </CardFooter>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Lựa chọn và công tắc</CardTitle>
                <CardDescription>
                  Vùng chạm vẫn đủ lớn khi dấu kiểm hoặc chấm chọn được vẽ nhỏ.
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-6">
                <div className="flex items-start gap-3">
                  <Checkbox
                    id="gallery-attendance"
                    checked={checked}
                    onCheckedChange={(value) => setChecked(value === true)}
                  />
                  <Label htmlFor="gallery-attendance" className="pt-3 leading-relaxed">
                    Ghi học sinh này là có mặt trong buổi học hôm nay
                  </Label>
                </div>
                <fieldset className="grid min-w-0 gap-2">
                  <legend className="mb-1 text-sm font-medium">Cách nhắc lịch</legend>
                  <RadioGroup defaultValue="morning">
                    {[
                      ['morning', 'Nhắc vào buổi sáng'],
                      ['before', 'Nhắc trước giờ học'],
                    ].map(([value, label]) => (
                      <div key={value} className="flex items-center gap-3">
                        <RadioGroupItem id={`gallery-radio-${value}`} value={value} />
                        <Label htmlFor={`gallery-radio-${value}`}>{label}</Label>
                      </div>
                    ))}
                  </RadioGroup>
                </fieldset>
                <div className="flex flex-wrap items-center justify-between gap-4 rounded-lg border border-border p-4">
                  <div className="grid min-w-0 flex-1 gap-1">
                    <Label htmlFor="gallery-notices">Thông báo thay đổi lịch</Label>
                    <p className="text-sm text-muted-foreground">
                      Chỉ thay đổi giao diện mẫu trong tab này.
                    </p>
                  </div>
                  <Switch id="gallery-notices" checked={notices} onCheckedChange={setNotices} />
                </div>
              </CardContent>
            </Card>
          </div>
        </Section>

        <Section
          id="classes"
          eyebrow="04 · Màu lớp"
          title="Bảy gia đình màu, một hợp đồng nhận diện"
          description="Mỗi lớp dùng nền nhạt, vạch cạnh mạnh và chữ thông thường có độ tương phản cao. Màu không bao giờ đứng một mình."
        >
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {CLASS_COLORS.map((color, index) => (
              <ClassColorCard
                key={color}
                color={color}
                title={`${CLASS_COLOR_LABELS[color].vi} · Lớp ${index + 6}A`}
                detail={index % 2 === 0 ? 'Thứ hai và thứ năm' : 'Thứ ba và thứ bảy'}
              />
            ))}
          </div>
        </Section>

        <Section
          id="data"
          eyebrow="05 · Dữ liệu"
          title="Bảng trên màn hình rộng, thẻ đọc tự nhiên trên điện thoại"
          description="Người gọi quyết định cách trình bày thẻ điện thoại. Thành phần không tự đoán cột nào có thể biến mất."
        >
          <Tabs defaultValue="present">
            <TabsList aria-label="Bộ lọc điểm danh">
              <TabsTrigger value="present">Đã có mặt</TabsTrigger>
              <TabsTrigger value="pending">Chờ điểm danh</TabsTrigger>
              <TabsTrigger value="disabled" disabled>
                Đã khóa
              </TabsTrigger>
            </TabsList>
            <TabsContent value="present">
              <GalleryStudentTable
                rows={galleryRows}
                emptyState={
                  <EmptyState
                    icon={<Users className="size-icon-lg" />}
                    title="Chưa có học sinh"
                    description="Thêm học sinh đầu tiên để bắt đầu danh sách lớp."
                    action={<Button>Thêm học sinh</Button>}
                  />
                }
              />
            </TabsContent>
            <TabsContent value="pending">
              <GalleryStudentTable
                rows={[]}
                emptyState={
                  <EmptyState
                    icon={<Check className="size-icon-lg" />}
                    title="Không còn ai chờ điểm danh"
                    description="Mọi học sinh trong buổi này đã có trạng thái rõ ràng."
                  />
                }
              />
            </TabsContent>
          </Tabs>
          <ScheduleGridExample />
        </Section>

        <Section
          id="overlays"
          eyebrow="06 · Lớp nổi"
          title="Lớp nổi giữ và trả lại tiêu điểm"
          description="Hộp thoại, bảng trượt, menu và chú giải dùng mô hình bàn phím của Radix. Nội dung và tên đóng luôn do người gọi cung cấp."
        >
          <Card>
            <CardContent className="flex flex-wrap gap-3 pt-5 md:pt-6">
              <Dialog>
                <DialogTrigger asChild>
                  <Button>Mở hộp thoại</Button>
                </DialogTrigger>
                <DialogContent closeLabel="Đóng hộp thoại xác nhận">
                  <DialogHeader>
                    <DialogTitle>Xác nhận buổi học</DialogTitle>
                    <DialogDescription>
                      Buổi học sẽ được ghi vào lịch và sẵn sàng để điểm danh.
                    </DialogDescription>
                  </DialogHeader>
                  <DialogFooter>
                    <DialogClose asChild>
                      <Button variant="secondary">Quay lại</Button>
                    </DialogClose>
                    <DialogClose asChild>
                      <Button>Xác nhận buổi học</Button>
                    </DialogClose>
                  </DialogFooter>
                </DialogContent>
              </Dialog>

              <Sheet>
                <SheetTrigger asChild>
                  <Button variant="secondary">Mở bảng trượt</Button>
                </SheetTrigger>
                <SheetContent side="end" closeLabel="Đóng bảng chi tiết">
                  <SheetHeader>
                    <SheetTitle>Chi tiết buổi học</SheetTitle>
                    <SheetDescription>
                      Thông tin phụ vẫn ở gần màn hình đang làm việc.
                    </SheetDescription>
                  </SheetHeader>
                  <ClassColorCard color="orange" title="Hóa học 11" detail="16:30 đến 18:00" />
                </SheetContent>
              </Sheet>

              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="quiet">
                    <CircleHelp aria-hidden="true" className="size-icon-sm" />
                    Giữ chuột hoặc đặt tiêu điểm
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Chú giải cũng xuất hiện bằng bàn phím.</TooltipContent>
              </Tooltip>

              <Button
                variant="secondary"
                onClick={() =>
                  toast.success('Đã lưu thay đổi', {
                    description: 'Lịch học đã dùng thông tin mới.',
                  })
                }
              >
                Hiện thông báo
              </Button>
            </CardContent>
          </Card>
        </Section>

        <Section
          id="feedback"
          eyebrow="07 · Phản hồi"
          title="Đang tải, trống và lỗi không bao giờ là khoảng trắng"
          description="Mỗi trạng thái có hình dáng, lời giải thích và mức thông báo phù hợp với tác động của nó."
        >
          <div className="grid gap-5 lg:grid-cols-3">
            <Card aria-busy="true" aria-label="Đang tải danh sách lớp">
              <CardHeader>
                <Skeleton className="h-5 w-2/3" />
                <Skeleton className="h-4 w-full" />
              </CardHeader>
              <CardContent className="grid gap-3">
                <Skeleton className="h-11 w-full" />
                <Skeleton className="h-11 w-full" />
                <span role="status" className="sr-only">
                  Đang tải danh sách lớp
                </span>
              </CardContent>
            </Card>
            <EmptyState
              icon={<CalendarDays className="size-icon-lg" />}
              title="Hôm nay chưa có buổi học"
              description="Tạo một buổi mới hoặc tận dụng khoảng trống để chuẩn bị bài."
              action={<Button variant="secondary">Tạo buổi học</Button>}
            />
            <ErrorState
              title="Không đọc được lịch học"
              description="Kết nối tới máy chủ đã ngắt. Hãy kiểm tra mạng rồi thử lại."
              action={<Button variant="secondary">Thử lại</Button>}
            />
          </div>
        </Section>

        <Section
          id="motion"
          eyebrow="08 · Chuyển động"
          title="Một nhịp vào trang, không có hiệu ứng rải rác"
          description="Các khối trong trang dùng một mẫu GSAP ngắn. Khi hệ điều hành yêu cầu giảm chuyển động, nội dung xuất hiện ngay ở trạng thái cuối."
        >
          <Alert>
            <AlertTitle>Hãy thử thay đổi cài đặt giảm chuyển động</AlertTitle>
            <AlertDescription>
              Tải lại trang để kiểm tra rằng không có nội dung nào bị ẩn hoặc chậm tương tác.
            </AlertDescription>
          </Alert>
        </Section>
      </PageEntrance>
    </AppShell>
  )
}
