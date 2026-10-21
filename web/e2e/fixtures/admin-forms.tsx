import { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Field, FieldLabel, FieldDescription, FieldError } from '../../src/components/ui/field'
import { Input } from '../../src/components/ui/input'
import { NativeSelect } from '../../src/components/ui/native-select'
import { Textarea } from '../../src/components/ui/textarea'
import { Switch } from '../../src/components/ui/switch'
import { Button } from '../../src/components/ui/button'
import '../../src/index.css'

const fieldKinds = [
  { name: 'input', Component: Input },
  { name: 'select', Component: NativeSelect },
  { name: 'textarea', Component: Textarea },
] as const

export function Fixture() {
  const [invalid, setInvalid] = useState(false)
  return (
    <main>
      {fieldKinds.map(({ name, Component }) => (
        <section key={name}>
          <p id={`${name}-plain-external`}>External plain description</p>
          <Field>
            <FieldLabel>{`${name} plain`}</FieldLabel>
            <Component id={`${name}-plain`} aria-describedby={`${name}-plain-external`} aria-invalid="true">
              {name === 'select' ? <option value="one">One</option> : undefined}
            </Component>
          </Field>

          <p id={`${name}-hint-external`}>External hint description</p>
          <Field>
            <FieldLabel>{`${name} hint`}</FieldLabel>
            <Component id={`${name}-hint`} aria-describedby={`${name}-hint-external ${name}-hint-external`} aria-invalid="true">
              {name === 'select' ? <option value="one">One</option> : undefined}
            </Component>
            <FieldDescription id={`${name}-hint-description`}>Local hint</FieldDescription>
          </Field>

          <p id={`${name}-error-external`}>External error description</p>
          <Field invalid>
            <FieldLabel>{`${name} error`}</FieldLabel>
            <Component id={`${name}-error`} aria-describedby={`${name}-error-external`} aria-invalid="false">
              {name === 'select' ? <option value="one">One</option> : undefined}
            </Component>
            <FieldError match id={`${name}-error-description`}>Local error</FieldError>
          </Field>
        </section>
      ))}
      <Field invalid={invalid}>
        <FieldLabel>Generated control</FieldLabel>
        <Input />
        {invalid ? <FieldError match>Try another value</FieldError> : <FieldDescription>Enter a value</FieldDescription>}
      </Field>
      <Button onClick={() => setInvalid(value => !value)}>Toggle error</Button>
      <Field orientation="horizontal">
        <Switch />
        <FieldLabel>Enabled feature</FieldLabel>
      </Field>
    </main>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Fixture />
  </StrictMode>,
)
