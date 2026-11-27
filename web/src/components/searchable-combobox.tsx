import { useMemo } from "react"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
} from "@/components/ui/combobox"
import { cn } from "@/lib/utils"

export interface ComboboxOption {
  value: string
  label: string
  description?: string
}

export interface SearchableComboboxProps {
  id?: string
  ariaLabel?: string
  ariaDescribedBy?: string
  options: ComboboxOption[]
  value: string
  onValueChange: (value: string) => void
  placeholder?: string
  emptyMessage?: string
  disabled?: boolean
  invalid?: boolean
  showClear?: boolean
  className?: string
}

export function SearchableCombobox({
  id,
  ariaLabel,
  ariaDescribedBy,
  options,
  value,
  onValueChange,
  placeholder = "Select option...",
  emptyMessage = "No matching option.",
  disabled = false,
  invalid = false,
  showClear = false,
  className,
}: SearchableComboboxProps) {
  const selected = useMemo(
    () => options.find((opt) => opt.value === value) ?? null,
    [options, value]
  )

  return (
    <Combobox<ComboboxOption>
      items={options}
      value={selected}
      onValueChange={(next) => {
        if (next) {
          onValueChange(next.value)
        } else if (showClear) {
          onValueChange("")
        }
      }}
      itemToStringLabel={(item) => item.label}
      itemToStringValue={(item) => item.value}
      isItemEqualToValue={(a, b) => a.value === b.value}
      disabled={disabled}
      autoHighlight
    >
      <ComboboxInput
        id={id}
        aria-label={ariaLabel}
        aria-describedby={ariaDescribedBy}
        aria-invalid={invalid ? "true" : "false"}
        placeholder={placeholder}
        disabled={disabled}
        showClear={showClear && Boolean(value)}
        className={cn("w-full", className)}
      />
      <ComboboxContent>
        <ComboboxEmpty>{emptyMessage}</ComboboxEmpty>
        <ComboboxList>
          {(item: ComboboxOption) => (
            <ComboboxItem key={item.value} value={item}>
              <div className="flex flex-col min-w-0">
                <span className="truncate">{item.label}</span>
                {item.description && (
                  <span className="text-[10px] text-muted-foreground truncate">
                    {item.description}
                  </span>
                )}
              </div>
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
