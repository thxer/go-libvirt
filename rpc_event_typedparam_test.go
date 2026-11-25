// Copyright 2026 The go-libvirt Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package libvirt

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/digitalocean/go-libvirt/internal/constants"
	"github.com/digitalocean/go-libvirt/internal/event"
	"github.com/digitalocean/go-libvirt/socket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jobCompletedWire is a remote_domain_event_callback_job_completed_msg as
// libvirtd sends it after a migration: callbackID, domain, and typed params.
func jobCompletedWire(callbackID int32) []byte {
	var b bytes.Buffer
	be := func(v any) { _ = binary.Write(&b, binary.BigEndian, v) }
	str := func(s string) {
		be(uint32(len(s)))
		b.WriteString(s)
		for i := len(s); i%4 != 0; i++ {
			b.WriteByte(0)
		}
	}
	be(callbackID)
	str("vm1")                              // dom.name
	b.Write(bytes.Repeat([]byte{0xab}, 16)) // dom.uuid
	be(int32(-1))                           // dom.id
	be(uint32(2))                           // params count
	str("operation")                        // field
	be(int32(TypedParamInt))
	be(int32(3)) // VIR_DOMAIN_JOB_OPERATION_MIGRATION_OUT
	str("downtime")
	be(int32(TypedParamUllong))
	be(uint64(1157))
	return b.Bytes()
}

// Events whose payload carries typed params (JOB_COMPLETED, TUNABLE) must be
// decoded with the TypedParam decoder, like RPC replies are; a plain XDR
// decoder cannot fill TypedParamValue's interface and Route used to drop
// the event silently.
func TestRouteJobCompletedEvent(t *testing.T) {
	const id = int32(7)
	l := &Libvirt{events: make(map[int32]*event.Stream)}
	stream := event.NewStream(constants.Program, id)
	l.addStream(stream)
	defer stream.Shutdown()

	h := &socket.Header{
		Program:   constants.Program,
		Procedure: constants.ProcDomainEventCallbackJobCompleted,
		Status:    socket.StatusOK,
	}
	go l.Route(h, jobCompletedWire(id))

	select {
	case e := <-stream.Recv():
		msg, ok := e.(*DomainEventCallbackJobCompletedMsg)
		require.True(t, ok, "got %T", e)
		assert.Equal(t, "vm1", msg.Dom.Name)
		require.Len(t, msg.Params, 2)
		assert.Equal(t, "operation", msg.Params[0].Field)
		assert.Equal(t, int32(3), msg.Params[0].Value.I)
		assert.Equal(t, "downtime", msg.Params[1].Field)
		assert.Equal(t, uint64(1157), msg.Params[1].Value.I)
	case <-time.After(2 * time.Second):
		t.Fatal("Route dropped the JOB_COMPLETED event")
	}
}
