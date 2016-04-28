package dtos

import (
	"fmt"
	"io"
)

type SingularityTaskShellCommandHistory struct {
	present      map[string]bool
	ShellRequest *SingularityTaskShellCommandRequest   `json:"shellRequest"`
	ShellUpdates SingularityTaskShellCommandUpdateList `json:"shellUpdates"`
}

func (self *SingularityTaskShellCommandHistory) Populate(jsonReader io.ReadCloser) (err error) {
	return ReadPopulate(jsonReader, self)
}

func (self *SingularityTaskShellCommandHistory) MarshalJSON() ([]byte, error) {
	return MarshalJSON(self)
}

func (self *SingularityTaskShellCommandHistory) FormatText() string {
	return FormatText(self)
}

func (self *SingularityTaskShellCommandHistory) FormatJSON() string {
	return FormatJSON(self)
}

func (self *SingularityTaskShellCommandHistory) FieldsPresent() []string {
	return presenceFromMap(self.present)
}

func (self *SingularityTaskShellCommandHistory) SetField(name string, value interface{}) error {
	if self.present == nil {
		self.present = make(map[string]bool)
	}
	switch name {
	default:
		return fmt.Errorf("No such field %s on SingularityTaskShellCommandHistory", name)

	case "shellRequest", "ShellRequest":
		v, ok := value.(*SingularityTaskShellCommandRequest)
		if ok {
			self.ShellRequest = v
			self.present["shellRequest"] = true
			return nil
		} else {
			return fmt.Errorf("Field shellRequest/ShellRequest: value %v(%T) couldn't be cast to type *SingularityTaskShellCommandRequest", value, value)
		}

	case "shellUpdates", "ShellUpdates":
		v, ok := value.(SingularityTaskShellCommandUpdateList)
		if ok {
			self.ShellUpdates = v
			self.present["shellUpdates"] = true
			return nil
		} else {
			return fmt.Errorf("Field shellUpdates/ShellUpdates: value %v(%T) couldn't be cast to type SingularityTaskShellCommandUpdateList", value, value)
		}

	}
}

func (self *SingularityTaskShellCommandHistory) GetField(name string) (interface{}, error) {
	switch name {
	default:
		return nil, fmt.Errorf("No such field %s on SingularityTaskShellCommandHistory", name)

	case "shellRequest", "ShellRequest":
		if self.present != nil {
			if _, ok := self.present["shellRequest"]; ok {
				return self.ShellRequest, nil
			}
		}
		return nil, fmt.Errorf("Field ShellRequest no set on ShellRequest %+v", self)

	case "shellUpdates", "ShellUpdates":
		if self.present != nil {
			if _, ok := self.present["shellUpdates"]; ok {
				return self.ShellUpdates, nil
			}
		}
		return nil, fmt.Errorf("Field ShellUpdates no set on ShellUpdates %+v", self)

	}
}

func (self *SingularityTaskShellCommandHistory) ClearField(name string) error {
	if self.present == nil {
		self.present = make(map[string]bool)
	}
	switch name {
	default:
		return fmt.Errorf("No such field %s on SingularityTaskShellCommandHistory", name)

	case "shellRequest", "ShellRequest":
		self.present["shellRequest"] = false

	case "shellUpdates", "ShellUpdates":
		self.present["shellUpdates"] = false

	}

	return nil
}

func (self *SingularityTaskShellCommandHistory) LoadMap(from map[string]interface{}) error {
	return loadMapIntoDTO(from, self)
}

type SingularityTaskShellCommandHistoryList []*SingularityTaskShellCommandHistory

func (list *SingularityTaskShellCommandHistoryList) Populate(jsonReader io.ReadCloser) (err error) {
	return ReadPopulate(jsonReader, list)
}

func (list *SingularityTaskShellCommandHistoryList) FormatText() string {
	text := []byte{}
	for _, dto := range *list {
		text = append(text, (*dto).FormatText()...)
		text = append(text, "\n"...)
	}
	return string(text)
}

func (list *SingularityTaskShellCommandHistoryList) FormatJSON() string {
	return FormatJSON(list)
}