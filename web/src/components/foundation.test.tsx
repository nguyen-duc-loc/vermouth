import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Plus } from 'lucide-react'
import { describe, expect, it } from 'vitest'

import { ClassColorCard } from './ClassColorCard'
import { EmptyState } from './EmptyState'
import { ErrorState } from './ErrorState'
import { FormField } from './FormField'
import { IconButton } from './IconButton'
import { ResponsiveTable } from './ResponsiveTable'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { TooltipProvider } from './ui/tooltip'

describe('foundation actions and states', () => {
  it('AC-4 keeps a loading action named, busy, and disabled', () => {
    render(<Button loading>Đang lưu lớp học</Button>)

    const button = screen.getByRole('button', { name: 'Đang lưu lớp học' })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('aria-busy', 'true')
  })

  it('AC-4 renders a slotted link as one named control', () => {
    render(
      <Button asChild>
        <a href="/next">
          <Plus aria-hidden="true" />
          Thêm buổi học
        </a>
      </Button>,
    )

    expect(screen.getByRole('link', { name: 'Thêm buổi học' })).toHaveAttribute('href', '/next')
  })

  it('AC-5 gives an icon action the caller supplied accessible name', () => {
    render(
      <TooltipProvider>
        <IconButton label="Thêm buổi học" icon={<Plus aria-hidden="true" />} />
      </TooltipProvider>,
    )

    expect(screen.getByRole('button', { name: 'Thêm buổi học' })).toBeEnabled()
  })

  it('AC-5 links a field label, help, and error to its control', () => {
    render(
      <FormField
        controlId="class-rate"
        label="Học phí mỗi buổi"
        hint="Nhập số tiền bằng đồng."
        error="Học phí phải lớn hơn 0 đồng."
        requiredText="(bắt buộc)"
        control={(accessibility) => <Input {...accessibility} defaultValue="0" />}
      />,
    )

    const input = screen.getByRole('textbox', { name: 'Học phí mỗi buổi(bắt buộc)' })
    expect(input).toBeInvalid()
    expect(input).toHaveAccessibleDescription(
      'Nhập số tiền bằng đồng. Học phí phải lớn hơn 0 đồng.',
    )
    expect(screen.getByRole('alert')).toHaveTextContent('Học phí phải lớn hơn 0 đồng.')
  })

  it('AC-4 distinguishes an empty result from a blocking failure', async () => {
    const user = userEvent.setup()
    render(
      <div>
        <EmptyState
          title="Chưa có học sinh"
          description="Thêm học sinh đầu tiên."
          action={<Button>Thêm học sinh</Button>}
        />
        <ErrorState
          title="Không đọc được lịch học"
          description="Hãy kiểm tra mạng rồi thử lại."
          action={<Button>Thử lại</Button>}
        />
      </div>,
    )

    expect(screen.getByText('Chưa có học sinh')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Không đọc được lịch học')
    await user.click(screen.getByRole('button', { name: 'Thử lại' }))
  })

  it('AC-6 uses caller supplied cards and semantic table content for the same rows', () => {
    const rows = [{ id: 'student-1', name: 'Nguyễn Minh Anh' }]
    render(
      <ResponsiveTable
        rows={rows}
        getRowKey={(row) => row.id}
        emptyState={<p>Chưa có dữ liệu</p>}
        renderCard={(row) => <article>Thẻ {row.name}</article>}
        renderTable={(tableRows) => (
          <table>
            <tbody>
              {tableRows.map((row) => (
                <tr key={row.id}>
                  <td>{row.name}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      />,
    )

    expect(screen.getByRole('article')).toHaveTextContent('Thẻ Nguyễn Minh Anh')
    expect(within(screen.getByRole('table')).getByText('Nguyễn Minh Anh')).toBeInTheDocument()
  })

  it('AC-6 returns the caller supplied empty state without data renderers', () => {
    render(
      <ResponsiveTable
        rows={[]}
        getRowKey={() => 'unused'}
        emptyState={<p>Chưa có dữ liệu</p>}
        renderCard={() => <p>Không được render</p>}
        renderTable={() => <p>Không được render</p>}
      />,
    )

    expect(
      screen.getByText('Chưa có dữ liệu').closest('[data-responsive-table-state]'),
    ).toHaveAttribute('data-responsive-table-state', 'empty')
    expect(screen.queryByText('Không được render')).not.toBeInTheDocument()
  })

  it('AC-10 renders class identity with its name and stable color value', () => {
    render(<ClassColorCard color="orange" title="Hóa học 11" detail="16:30 đến 18:00" />)

    const card = screen.getByRole('article')
    expect(card).toHaveAttribute('data-class-color', 'orange')
    expect(card).toHaveTextContent('Hóa học 11')
    expect(card).toHaveTextContent('16:30 đến 18:00')
  })
})
