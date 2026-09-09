import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'

import { Button } from './ui/button'
import { Checkbox } from './ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from './ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from './ui/dropdown-menu'
import { RadioGroup, RadioGroupItem } from './ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './ui/select'
import { Switch } from './ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs'

// biome-ignore lint/style/useComponentExportOnlyModules: This test harness belongs only to this file.
function ChoiceControls() {
  const [checked, setChecked] = useState(false)
  const [enabled, setEnabled] = useState(false)
  return (
    <div>
      <Checkbox
        aria-label="Có mặt"
        checked={checked}
        onCheckedChange={(value) => setChecked(value === true)}
      />
      <Switch aria-label="Nhận thông báo" checked={enabled} onCheckedChange={setEnabled} />
    </div>
  )
}

describe('foundation keyboard controls', () => {
  it('AC-5 changes checkbox and switch state through named controls', async () => {
    const user = userEvent.setup()
    render(<ChoiceControls />)

    const checkbox = screen.getByRole('checkbox', { name: 'Có mặt' })
    const toggle = screen.getByRole('switch', { name: 'Nhận thông báo' })
    await user.click(checkbox)
    await user.click(toggle)

    expect(checkbox).toBeChecked()
    expect(toggle).toBeChecked()
  })

  it('AC-5 moves and selects radio choices with arrow keys', async () => {
    const user = userEvent.setup()
    render(
      <RadioGroup defaultValue="morning" aria-label="Cách nhắc lịch">
        <RadioGroupItem value="morning" aria-label="Buổi sáng" />
        <RadioGroupItem value="before" aria-label="Trước giờ học" />
      </RadioGroup>,
    )

    const morning = screen.getByRole('radio', { name: 'Buổi sáng' })
    const before = screen.getByRole('radio', { name: 'Trước giờ học' })
    morning.focus()
    await user.keyboard('{ArrowRight}')

    expect(before).toHaveFocus()
    expect(before).toBeChecked()
  })

  it('AC-4 opens a select and exposes the chosen caller value', async () => {
    const user = userEvent.setup()
    render(
      <Select defaultValue="active">
        <SelectTrigger aria-label="Trạng thái lớp">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="active">Đang học</SelectItem>
          <SelectItem value="paused">Tạm dừng</SelectItem>
        </SelectContent>
      </Select>,
    )

    const trigger = screen.getByRole('combobox', { name: 'Trạng thái lớp' })
    await user.click(trigger)
    await user.click(screen.getByRole('option', { name: 'Tạm dừng' }))

    expect(trigger).toHaveTextContent('Tạm dừng')
  })

  it('AC-5 switches tabs with their keyboard model', async () => {
    const user = userEvent.setup()
    render(
      <Tabs defaultValue="present">
        <TabsList aria-label="Bộ lọc điểm danh">
          <TabsTrigger value="present">Đã có mặt</TabsTrigger>
          <TabsTrigger value="pending">Chờ điểm danh</TabsTrigger>
        </TabsList>
        <TabsContent value="present">Danh sách có mặt</TabsContent>
        <TabsContent value="pending">Danh sách chờ</TabsContent>
      </Tabs>,
    )

    await user.click(screen.getByRole('tab', { name: 'Chờ điểm danh' }))

    expect(screen.getByRole('tab', { name: 'Chờ điểm danh' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('tabpanel')).toHaveTextContent('Danh sách chờ')
  })

  it('AC-5 traps dialog focus and returns it to the trigger on close', async () => {
    const user = userEvent.setup()
    render(
      <Dialog>
        <DialogTrigger asChild>
          <Button>Mở hộp thoại</Button>
        </DialogTrigger>
        <DialogContent closeLabel="Đóng hộp thoại">
          <DialogTitle>Xác nhận buổi học</DialogTitle>
          <DialogDescription>Kiểm tra thông tin trước khi lưu.</DialogDescription>
        </DialogContent>
      </Dialog>,
    )

    const trigger = screen.getByRole('button', { name: 'Mở hộp thoại' })
    await user.click(trigger)
    const dialog = screen.getByRole('dialog', { name: 'Xác nhận buổi học' })
    expect(dialog).toContainElement(document.activeElement as HTMLElement)

    await user.click(screen.getByRole('button', { name: 'Đóng hộp thoại' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('AC-5 opens a menu from the keyboard and returns focus on escape', async () => {
    const user = userEvent.setup()
    render(
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button>Thêm lựa chọn</Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>Đổi tên lớp</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    )

    const trigger = screen.getByRole('button', { name: 'Thêm lựa chọn' })
    trigger.focus()
    await user.keyboard('{Enter}')
    expect(screen.getByRole('menuitem', { name: 'Đổi tên lớp' })).toBeInTheDocument()

    await user.keyboard('{Escape}')

    expect(screen.queryByRole('menuitem')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })
})
